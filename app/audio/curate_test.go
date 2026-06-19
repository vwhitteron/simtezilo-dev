package audio

import "testing"

func TestParseCardIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want int
	}{
		{"hifiberry hw token", "snd_rpi_hifiberry_dac: HifiBerry DAC HiFi pcm5102a-hifi-0 (hw:0,0)", 0},
		{"second card", "USB Audio Device: USB Audio (hw:2,0)", 2},
		{"no token", "default", -1},
		{"bluealsa no token", "bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp", -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := parseCardIndex(tc.in); got != tc.want {
				t.Fatalf("parseCardIndex(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestBTAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"a2dp pcm", "bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp", "90:7A:58:D9:14:B3"},
		{"lowercase hex upcased", "bluealsa:DEV=aa:bb:cc:dd:ee:ff,PROFILE=a2dp", "AA:BB:CC:DD:EE:FF"},
		{"no dev token", "default", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := BTAddress(tc.in); got != tc.want {
				t.Fatalf("BTAddress(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestClassifyType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want DeviceType
	}{
		{"bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp", DeviceBluetooth},
		{"Bluetooth Speaker", DeviceBluetooth},
		{"USB Audio Device (hw:1,0)", DeviceUSB},
		{"vc4-hdmi: HDMI (hw:2,0)", DeviceHDMI},
		{"snd_rpi_hifiberry_dac: HifiBerry DAC HiFi pcm5102a-hifi-0 (hw:0,0)", DeviceBuiltin},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			if got := classifyType(tc.in); got != tc.want {
				t.Fatalf("classifyType(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFriendlyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"hifiberry chip map", "snd_rpi_hifiberry_dac: HifiBerry DAC HiFi pcm5102a-hifi-0 (hw:0,0)", "HifiBerry DAC (PCM5102a)"},
		{"headphone jack", "bcm2835 Headphones: - (hw:0,0)", "3.5mm Headphone Jack"},
		{"hdmi", "vc4-hdmi: HDMI (hw:2,0)", "HDMI"},
		{"usb fallback prettify", "USB Audio Device: USB Audio (hw:1,0)", "USB Audio"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := friendlyName(tc.in); got != tc.want {
				t.Fatalf("friendlyName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsVirtualAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want bool
	}{
		{"default", true},
		{"sysdefault", true},
		{"dmix", true},
		{"dmix:CARD=sndrpihifiberry", true},
		{"surround40", true},
		{"surround51:CARD=sndrpihifiberry", true},
		{"default 128ch", true},
		{"bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp", false},
		{"snd_rpi_hifiberry_dac: HifiBerry DAC HiFi pcm5102a-hifi-0 (hw:0,0)", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			if got := isVirtualAlias(tc.in); got != tc.want {
				t.Fatalf("isVirtualAlias(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCurateLocal(t *testing.T) {
	t.Parallel()

	// Mirrors the raw PortAudio enumeration seen on a Pi: one card shown as a
	// raw hw entry plus several virtual aliases, plus a connected BT speaker.
	raw := []Device{
		{ID: "0", Name: "snd_rpi_hifiberry_dac: HifiBerry DAC HiFi pcm5102a-hifi-0 (hw:0,0)", MaxChannels: 2},
		{ID: "1", Name: "sysdefault", MaxChannels: 128},
		{ID: "2", Name: "default", MaxChannels: 128, IsDefault: true},
		{ID: "3", Name: "dmix", MaxChannels: 2},
		{ID: "4", Name: "bluealsa:DEV=90:7A:58:D9:14:B3,PROFILE=a2dp", MaxChannels: 2},
	}

	got := CurateLocal(raw)

	if len(got) != 2 {
		t.Fatalf("CurateLocal returned %d devices, want 2 (one card + one BT): %+v", len(got), got)
	}

	// Built-in card first, Bluetooth second (type ordering).
	card := got[0]
	if card.Type != DeviceBuiltin {
		t.Fatalf("first device type = %q, want builtin", card.Type)
	}

	if card.DisplayName != "HifiBerry DAC (PCM5102a)" {
		t.Fatalf("card DisplayName = %q, want friendly label", card.DisplayName)
	}

	if card.ID != "0" {
		t.Fatalf("card representative ID = %q, want the hw entry %q", card.ID, "0")
	}

	if card.Name != raw[0].Name {
		t.Fatalf("card Name = %q, want stable raw name %q", card.Name, raw[0].Name)
	}

	bt := got[1]
	if bt.Type != DeviceBluetooth {
		t.Fatalf("second device type = %q, want bluetooth", bt.Type)
	}

	if addr := BTAddress(bt.Name); addr != "90:7A:58:D9:14:B3" {
		t.Fatalf("BT device address = %q, want MAC", addr)
	}
}

func TestCurateLocalLoopback(t *testing.T) {
	t.Parallel()

	// snd-aloop exposes both loopback devices to PortAudio; only device 0 (the
	// app's playback side) should surface, tagged Bluetooth.
	raw := []Device{
		{ID: "0", Name: "snd_rpi_hifiberry_dac: HifiBerry DAC HiFi pcm5102a-hifi-0 (hw:0,0)", MaxChannels: 2},
		{ID: "5", Name: "Loopback: PCM (hw:1,0)", MaxChannels: 32},
		{ID: "6", Name: "Loopback: PCM (hw:1,1)", MaxChannels: 32},
	}

	got := CurateLocal(raw)

	var loop *Device

	for i := range got {
		if got[i].Type == DeviceBluetooth {
			loop = &got[i]
		}

		if got[i].Name == "Loopback: PCM (hw:1,1)" {
			t.Fatalf("device 1 (bridge capture side) must be hidden, got %+v", got[i])
		}
	}

	if loop == nil {
		t.Fatalf("expected a Bluetooth-tagged Loopback device, got %+v", got)
	}

	if loop.ID != "5" || loop.Name != "Loopback: PCM (hw:1,0)" {
		t.Fatalf("loopback device = %+v, want the hw:1,0 entry", *loop)
	}

	if loop.MaxChannels != 2 {
		t.Fatalf("loopback MaxChannels = %d, want 2 (stereo A2DP)", loop.MaxChannels)
	}
}

func TestParseCardDevice(t *testing.T) {
	t.Parallel()

	card, device, ok := parseCardDevice("Loopback: PCM (hw:1,1)")
	if !ok || card != 1 || device != 1 {
		t.Fatalf("parseCardDevice = (%d,%d,%v), want (1,1,true)", card, device, ok)
	}

	if _, _, ok := parseCardDevice("default"); ok {
		t.Fatalf("parseCardDevice(default) ok = true, want false")
	}
}
