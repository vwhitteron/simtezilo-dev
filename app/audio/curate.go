package audio

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Device curation turns the raw, redundant ALSA/PortAudio device list into a
// clean, one-entry-per-physical-output list with friendly display names and a
// semantic type. The functions here are pure (no portaudio/platform deps) so
// they are unit-testable on any host.

// hwTokenRE matches the "(hw:C,D)" card token PortAudio appends to real ALSA
// device names; group 1 is the card index, group 2 the device index.
var hwTokenRE = regexp.MustCompile(`\(hw:(\d+),(\d+)\)`)

// btDevRE matches the "DEV=AA:BB:CC:DD:EE:FF" address token in a bluealsa PCM
// name; group 1 is the MAC address.
var btDevRE = regexp.MustCompile(`(?i)DEV=([0-9A-F:]{17})`)

// virtualAliasPrefixes are ALSA plug/virtual PCM names that duplicate a real
// card without identifying one. They are dropped during curation; the UI offers
// a single "System default" entry for the default route instead.
var virtualAliasPrefixes = []string{
	"default", "sysdefault", "dmix", "dsnoop", "front", "surround",
	"samplerate", "speexrate", "pulse", "upmix", "vdownmix", "hdmi",
	"iec958", "spdif", "hw", "plughw", "usbstream", "null",
}

// parseCardIndex returns the ALSA card index embedded in a PortAudio device
// name, or -1 when the name carries no "(hw:C,D)" token.
func parseCardIndex(name string) int {
	card, _, ok := parseCardDevice(name)
	if !ok {
		return -1
	}

	return card
}

// parseCardDevice returns the ALSA card and device indices from a "(hw:C,D)"
// token. ok is false when the name carries no such token.
func parseCardDevice(name string) (card, device int, ok bool) {
	match := hwTokenRE.FindStringSubmatch(name)
	if match == nil {
		return 0, 0, false
	}

	card, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}

	device, err = strconv.Atoi(match[2])
	if err != nil {
		return 0, 0, false
	}

	return card, device, true
}

// isLoopback reports whether a device belongs to the snd-aloop "Loopback" card,
// which the platform helper uses to bridge audio to a Bluetooth speaker.
func isLoopback(name string) bool {
	return strings.Contains(strings.ToLower(name), "loopback")
}

// BTAddress extracts the Bluetooth MAC from a bluealsa PCM name (e.g.
// "bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp"), upper-cased, or "" if none.
func BTAddress(name string) string {
	m := btDevRE.FindStringSubmatch(name)
	if m == nil {
		return ""
	}

	return strings.ToUpper(m[1])
}

// classifyType maps a raw device name to a semantic DeviceType.
func classifyType(name string) DeviceType {
	lower := strings.ToLower(name)

	switch {
	case strings.Contains(lower, "bluealsa") || strings.Contains(lower, "bluetooth"):
		return DeviceBluetooth
	case strings.Contains(lower, "usb"):
		return DeviceUSB
	case strings.Contains(lower, "hdmi"):
		return DeviceHDMI
	default:
		return DeviceBuiltin
	}
}

// friendlyName derives a clean, human-facing label from a raw device name. It
// uses a small extensible map for known chips and falls back to prettifying the
// raw name so unknown hardware on other Pi models still reads sensibly.
func friendlyName(name string) string {
	lower := strings.ToLower(name)

	switch {
	case strings.Contains(lower, "pcm5102") || strings.Contains(lower, "hifiberry"):
		return "HifiBerry DAC (PCM5102a)"
	case strings.Contains(lower, "bcm2835") && strings.Contains(lower, "headphone"):
		return "3.5mm Headphone Jack"
	case strings.Contains(lower, "vc4-hdmi") || strings.Contains(lower, "hdmi"):
		return "HDMI"
	}

	return prettify(name)
}

// prettify cleans ALSA noise out of a raw device name: it keeps the descriptive
// portion after the leading "card_id:" token, drops the trailing "(hw:C,D)"
// token, and strips common prefixes/suffixes.
func prettify(name string) string {
	out := name

	// Drop the "(hw:C,D)" token and anything after it.
	if loc := hwTokenRE.FindStringIndex(out); loc != nil {
		out = out[:loc[0]]
	}

	// Keep the human description after the "card_id: " prefix.
	if idx := strings.Index(out, ": "); idx >= 0 {
		out = out[idx+2:]
	}

	out = strings.TrimPrefix(out, "snd_rpi_")
	out = strings.ReplaceAll(out, "-hifi-0", "")
	out = strings.TrimSpace(out)

	if out == "" {
		return name
	}

	return out
}

// isVirtualAlias reports whether a non-card-attributed name is a pure ALSA
// virtual/plug PCM that should be dropped. bluealsa is never treated as a
// virtual alias (it is a real, selectable Bluetooth output).
func isVirtualAlias(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))

	if strings.Contains(lower, "bluealsa") {
		return false
	}

	for _, prefix := range virtualAliasPrefixes {
		if lower == prefix {
			return true
		}

		// Match the prefix when it is followed by a non-letter — a digit
		// ("surround40", "surround51"), a colon ("dmix:CARD=..."), or a space
		// ("default 128ch") — while leaving unrelated names ("defaultfoo") alone.
		if strings.HasPrefix(lower, prefix) {
			next := lower[len(prefix)]
			if next < 'a' || next > 'z' {
				return true
			}
		}
	}

	return false
}

// CurateLocal collapses a raw backend device list into one entry per physical
// output. Card-attributed devices (those carrying a "(hw:C,D)" token) are
// grouped by card index and represented once; bluealsa PCMs are kept and tagged
// Bluetooth; redundant virtual aliases (default/dmix/sysdefault/...) are dropped.
//
// Each curated device keeps its representative's native ID (so OpenSink/Resolve
// stay consistent) and stable Name, gains a friendly DisplayName and a Type, and
// carries the representative's IsDefault flag if any grouped entry was default.
func CurateLocal(devices []Device) []Device {
	type group struct {
		rep      Device
		channels int
	}

	byCard := map[int]*group{}
	curated := make([]Device, 0, len(devices))

	for _, dev := range devices {
		typ := classifyType(dev.Name)

		// The snd-aloop Loopback card is the Bluetooth bridge. PortAudio lists
		// both of its devices; the app must play to device 0 (the bridge captures
		// device 1), so expose only device 0 and present it as the BT speaker.
		if isLoopback(dev.Name) {
			_, device, ok := parseCardDevice(dev.Name)
			if !ok || device != 0 {
				continue
			}

			loop := dev
			loop.Type = DeviceBluetooth
			loop.DisplayName = "Bluetooth"
			loop.MaxChannels = 2 // A2DP is stereo; the raw 32ch is a loopback artefact.
			curated = append(curated, loop)

			continue
		}

		// Bluetooth (bluealsa) outputs are real, selectable devices: keep one
		// entry each, keyed by the stable raw PCM name.
		if typ == DeviceBluetooth {
			curated = append(curated, curatedDevice(dev, typ))

			continue
		}

		card := parseCardIndex(dev.Name)
		if card < 0 {
			// No card identity: drop pure virtual aliases, keep anything else
			// (defensive — unusual named devices stay visible).
			if isVirtualAlias(dev.Name) {
				continue
			}

			curated = append(curated, curatedDevice(dev, typ))

			continue
		}

		existing, ok := byCard[card]
		if !ok {
			byCard[card] = &group{rep: dev, channels: dev.MaxChannels}

			continue
		}

		// Prefer the representative with the most channels (the raw hw entry),
		// and remember if any entry in the group was the system default.
		if dev.MaxChannels > existing.channels {
			existing.rep = dev
			existing.channels = dev.MaxChannels
		}

		if dev.IsDefault {
			existing.rep.IsDefault = true
		}
	}

	for _, g := range byCard {
		curated = append(curated, curatedDevice(g.rep, classifyType(g.rep.Name)))
	}

	sortCurated(curated)

	return curated
}

// curatedDevice returns dev with a friendly DisplayName and Type filled in,
// leaving the stable Name/ID untouched.
func curatedDevice(dev Device, typ DeviceType) Device {
	dev.Type = typ
	dev.DisplayName = friendlyName(dev.Name)

	return dev
}

// typeOrder ranks device types for stable, sensible UI grouping.
func typeOrder(t DeviceType) int {
	switch t {
	case DeviceBuiltin:
		return 0
	case DeviceUSB:
		return 1
	case DeviceHDMI:
		return 2
	case DeviceBluetooth:
		return 3
	default:
		return 4
	}
}

// sortCurated orders devices by type group, then by display name.
func sortCurated(devices []Device) {
	sort.SliceStable(devices, func(left, right int) bool {
		lo, ro := typeOrder(devices[left].Type), typeOrder(devices[right].Type)
		if lo != ro {
			return lo < ro
		}

		return strings.ToLower(devices[left].DisplayName) < strings.ToLower(devices[right].DisplayName)
	})
}
