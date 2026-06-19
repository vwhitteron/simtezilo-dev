package fancontroller

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout           = 5 * time.Second
	supportedProtocolVersion = uint16(1)

	// Fan Control opcodes (single-byte writes to the Fan Control characteristic).
	// See doc/fan-gatt-profile.md in the firmware repo.
	opcodeUnpair         = 0x01
	opcodeTakeControl    = 0x02
	opcodeReleaseControl = 0x03

	// displayTextMaxBytes is the maximum Display Text payload the device renders
	// (excluding any NUL). Longer writes are rejected by the firmware.
	displayTextMaxBytes = 64

	// Display Image transfer limits and frame tags. The display box caps the icon
	// at imageMaxWidth × imageMaxHeight; smaller icons are centred by the device.
	// Pixels are RGB565, little-endian, 2 bytes each. See doc/fan-gatt-profile.md.
	imageMaxWidth   = 150
	imageMaxHeight  = 50
	imageBytesPerPx = 2
	imgFrameBegin   = 0x01
	imgFrameData    = 0x02
	imgFrameCommit  = 0x03
	// imgDefaultChunk is the pixel-bytes-per-DATA-frame upper bound. It matches
	// the firmware's preferred ATT MTU of 256 (256 − 3 ATT − 4 frame header,
	// rounded down for headroom); the transport lowers it if the negotiated MTU
	// is smaller.
	imgDefaultChunk = 244
)

var errNotConnected = errors.New("windsim not connected")

// Event is a single-byte device→host notification from the Fan Event
// characteristic.
type Event uint8

const (
	// EventButton is delivered when the device's front button is pressed.
	EventButton Event = 0x01
	// EventImageOK is delivered when a Display Image transfer completed and was
	// rendered on the device screen.
	EventImageOK Event = 0x02
	// EventImageAbort is delivered when a Display Image transfer was rejected or
	// aborted (bad dimensions, a sequence gap, or an incomplete COMMIT). The host
	// should restart the transfer from BEGIN. See doc/fan-gatt-profile.md.
	EventImageAbort Event = 0x03
)

// device is the typed GATT interface between the controller and the fan
// hardware. Each method maps directly to a custom-UUID characteristic on the
// fan GATT profile; the BLE transport implements this interface and tests
// substitute a fake. See newDevice.
type device interface {
	// Connect establishes the link, subscribes to Fan Status and Fan Event
	// notifications, and starts the disconnect watcher. onStatus and onEvent are
	// each called from a single goroutine whenever a notification arrives and
	// must not block.
	Connect(ctx context.Context, onStatus func(Status), onEvent func(Event)) error
	// WriteDuty writes a duty value (0–100) to the Fan Duty characteristic
	// using write-without-response.
	WriteDuty(ctx context.Context, duty uint8) error
	// ReadStatus reads the Fan Status characteristic and returns the decoded
	// duty and RPM.
	ReadStatus(ctx context.Context) (Status, error)
	// ReadCapabilities reads the Fan Capabilities characteristic and returns
	// the decoded protocol version and flags.
	ReadCapabilities(ctx context.Context) (Capabilities, error)
	// SendControl writes a single-byte opcode to the Fan Control characteristic.
	SendControl(ctx context.Context, op uint8) error
	// WriteDisplayText writes a UTF-8 string to the Display Text characteristic
	// for the device to render on its screen. An empty string clears it.
	WriteDisplayText(ctx context.Context, text string) error
	// WriteDisplayImage streams a width×height little-endian RGB565 icon to the
	// Display Image characteristic as a BEGIN/DATA…/COMMIT frame sequence. The
	// icon replaces any text or image currently shown.
	WriteDisplayImage(ctx context.Context, pixels []byte, width, height int) error
	// Disconnected is closed when the underlying link drops.
	Disconnected() <-chan struct{}
	Close() error
}

// Status is the live fan state as reported by the Fan Status characteristic.
type Status struct {
	Duty int
	RPM  int
}

// Capabilities holds the values from the Fan Capabilities characteristic,
// read once on connect to validate firmware compatibility.
type Capabilities struct {
	ProtocolVersion uint16
	Flags           uint16
}

// Options configures a wind simulator client.
type Options struct {
	// Address is the device address (a MAC address on Linux/Windows). The
	// device must already be paired via the Bluetooth panel.
	Address        string
	CommandTimeout time.Duration
	// OnEvent, if set, is called for each Fan Event notification (front-button
	// press, control released). It runs on the BLE notification goroutine and
	// must not block.
	OnEvent func(Event)
}

// Client handles communication with a wind simulator device over the fan GATT
// profile.
type Client struct {
	options Options

	mu        sync.RWMutex
	dev       device
	connected bool
	address   string
	lastDuty  int

	statusMu   sync.Mutex
	lastStatus Status
	haveStatus bool
}

// New creates a wind simulator client.
func New(options Options) *Client {
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = defaultTimeout
	}

	return &Client{
		options:  options,
		lastDuty: -1,
	}
}

// Connect opens the GATT link to the configured device address, validates the
// firmware protocol version, and subscribes to status notifications.
func (c *Client) Connect(ctx context.Context) error {
	address := strings.TrimSpace(c.options.Address)
	if address == "" {
		return errors.New("no fan device address configured")
	}

	dev, err := newDevice(address)
	if err != nil {
		return err
	}

	err = dev.Connect(ctx, c.onStatus, c.onEvent)
	if err != nil {
		return err
	}

	caps, err := dev.ReadCapabilities(ctx)
	if err != nil {
		_ = dev.Close()

		return fmt.Errorf("read capabilities: %w", err)
	}

	if caps.ProtocolVersion == 0 || caps.ProtocolVersion > supportedProtocolVersion {
		_ = dev.Close()

		return fmt.Errorf("unsupported fan protocol version %d (supported: %d)",
			caps.ProtocolVersion, supportedProtocolVersion)
	}

	c.statusMu.Lock()
	c.haveStatus = false
	c.statusMu.Unlock()

	c.mu.Lock()
	c.dev = dev
	c.connected = true
	c.address = address
	c.lastDuty = -1
	c.mu.Unlock()

	return nil
}

// Close ends the active connection.
func (c *Client) Close() error {
	c.mu.Lock()
	dev := c.dev
	connected := c.connected
	c.connected = false
	c.dev = nil
	c.mu.Unlock()

	if !connected || dev == nil {
		return nil
	}

	err := dev.Close()
	if err != nil {
		return fmt.Errorf("close device: %w", err)
	}

	return nil
}

// Ping reads capabilities from the device as a liveness check.
func (c *Client) Ping(ctx context.Context) error {
	c.mu.RLock()
	dev := c.dev
	c.mu.RUnlock()

	if dev == nil {
		return errNotConnected
	}

	_, err := dev.ReadCapabilities(ctx)

	return err
}

// Unpair sends the UNPAIR opcode to the Fan Control characteristic,
// instructing the firmware to remove bonding state and disconnect.
func (c *Client) Unpair(ctx context.Context) error {
	return c.sendControl(ctx, opcodeUnpair)
}

// TakeControl sends the TAKE_CONTROL opcode: the host claims control of the fan
// and drives it with SetFanDuty until it releases. The device preserves the
// user's manual setpoint and restores it on release, disconnect, or when the
// user turns the encoder.
func (c *Client) TakeControl(ctx context.Context) error {
	return c.sendControl(ctx, opcodeTakeControl)
}

// ReleaseControl sends the RELEASE_CONTROL opcode: the host hands control back
// and the device restores the user's manual setpoint immediately.
func (c *Client) ReleaseControl(ctx context.Context) error {
	return c.sendControl(ctx, opcodeReleaseControl)
}

// SetDisplayText writes a UTF-8 string (≤64 bytes) to the Display Text
// characteristic for the device to render on its screen. An empty string clears
// the displayed text.
func (c *Client) SetDisplayText(ctx context.Context, text string) error {
	if len(text) > displayTextMaxBytes {
		return fmt.Errorf("display text too long: %d bytes (max %d)", len(text), displayTextMaxBytes)
	}

	c.mu.RLock()
	dev := c.dev
	c.mu.RUnlock()

	if dev == nil {
		return errNotConnected
	}

	return dev.WriteDisplayText(ctx, text)
}

// SetDisplayImage streams a little-endian RGB565 icon to the Display Image
// characteristic for the device to render on its screen. width×height must be
// within the device's display box (≤150×50) and pixels must be exactly
// width*height*2 bytes. The icon replaces any text or image currently shown.
func (c *Client) SetDisplayImage(ctx context.Context, pixels []byte, width, height int) error {
	if width < 1 || width > imageMaxWidth || height < 1 || height > imageMaxHeight {
		return fmt.Errorf("display image %dx%d out of range (max %dx%d)",
			width, height, imageMaxWidth, imageMaxHeight)
	}

	want := width * height * imageBytesPerPx
	if len(pixels) != want {
		return fmt.Errorf("display image is %d bytes, expected %d (%dx%d RGB565)",
			len(pixels), want, width, height)
	}

	c.mu.RLock()
	dev := c.dev
	c.mu.RUnlock()

	if dev == nil {
		return errNotConnected
	}

	return dev.WriteDisplayImage(ctx, pixels, width, height)
}

// SetFanDuty writes a duty value (0–100) to the Fan Duty characteristic.
// If the duty has not changed since the last successful call, the write is
// skipped and the cached value is returned.
func (c *Client) SetFanDuty(ctx context.Context, duty int) (int, error) {
	if duty < 0 || duty > 100 {
		return 0, fmt.Errorf("invalid duty %d: must be in range 0-100", duty)
	}

	if duty == c.lastDuty {
		return duty, nil
	}

	c.mu.RLock()
	dev := c.dev
	connected := c.connected
	c.mu.RUnlock()

	if !connected || dev == nil {
		return 0, errNotConnected
	}

	err := dev.WriteDuty(ctx, uint8(duty)) //nolint:gosec // duty is validated 0-100 above
	if err != nil {
		return 0, fmt.Errorf("write duty: %w", err)
	}

	c.lastDuty = duty

	return duty, nil
}

// GetStatus returns the most recently notified fan status. If no notification
// has been received yet, it falls back to a direct read of the Fan Status
// characteristic.
func (c *Client) GetStatus(ctx context.Context) (Status, error) {
	c.statusMu.Lock()
	have := c.haveStatus
	status := c.lastStatus
	c.statusMu.Unlock()

	if have {
		return status, nil
	}

	c.mu.RLock()
	dev := c.dev
	c.mu.RUnlock()

	if dev == nil {
		return Status{}, errNotConnected
	}

	return dev.ReadStatus(ctx)
}

// DeviceAddress returns the address of the connected device.
func (c *Client) DeviceAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.address
}

// Disconnected returns a channel that is closed when the connection drops.
// Returns nil if not connected.
func (c *Client) Disconnected() <-chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.dev == nil {
		return nil
	}

	return c.dev.Disconnected()
}

// sendControl writes a single-byte opcode to the Fan Control characteristic.
func (c *Client) sendControl(ctx context.Context, opcode uint8) error {
	c.mu.RLock()
	dev := c.dev
	c.mu.RUnlock()

	if dev == nil {
		return errNotConnected
	}

	return dev.SendControl(ctx, opcode)
}

// onStatus is the notification callback delivered by the device. It caches the
// latest status under the status mutex so GetStatus can return it without a
// round-trip read. It is called from the device's notification goroutine and
// must not block.
func (c *Client) onStatus(s Status) {
	c.statusMu.Lock()
	c.lastStatus = s
	c.haveStatus = true
	c.statusMu.Unlock()
}

// onEvent is the Fan Event callback delivered by the device. It forwards the
// event to the user-supplied Options.OnEvent, if any. It is called from the
// device's notification goroutine and must not block.
func (c *Client) onEvent(e Event) {
	if c.options.OnEvent != nil {
		c.options.OnEvent(e)
	}
}

// encodeDuty encodes a duty value as a single byte ready to write to the Fan
// Duty characteristic.
func encodeDuty(duty uint8) []byte {
	return []byte{duty}
}

// encodeImageFrames frames a width×height little-endian RGB565 image as the
// BEGIN / DATA… / COMMIT write sequence for the Display Image characteristic,
// returned in send order. gen is the transfer generation (wraps at 255); the
// device ignores frames whose gen does not match the most recent BEGIN. chunk is
// the pixel-bytes per DATA frame and must fit the negotiated ATT MTU minus the
// 4-byte frame header and 3-byte ATT header. It mirrors the firmware reassembler
// (see encode_image_transfer in the firmware repo's tools/fan_protocol.py).
func encodeImageFrames(pixels []byte, width, height int, gen uint8, chunk int) ([][]byte, error) {
	if width < 1 || width > imageMaxWidth || height < 1 || height > imageMaxHeight {
		return nil, fmt.Errorf("image dimensions %dx%d out of range (max %dx%d)",
			width, height, imageMaxWidth, imageMaxHeight)
	}

	total := width * height * imageBytesPerPx
	if len(pixels) != total {
		return nil, fmt.Errorf("pixel buffer is %d bytes, expected %d (%dx%d RGB565)",
			len(pixels), total, width, height)
	}

	if chunk < 1 {
		return nil, fmt.Errorf("chunk must be >= 1: %d", chunk)
	}

	frames := make([][]byte, 0, total/chunk+2)

	begin := make([]byte, 6)
	begin[0] = imgFrameBegin
	begin[1] = gen
	begin[2] = byte(width)
	begin[3] = byte(height)
	//nolint:gosec // total = width*height*2 ≤ 150*50*2 = 15000, fits uint16.
	binary.LittleEndian.PutUint16(begin[4:6], uint16(total))
	frames = append(frames, begin)

	for offset := 0; offset < total; offset += chunk {
		end := min(offset+chunk, total)

		part := pixels[offset:end]

		frame := make([]byte, 4+len(part))
		frame[0] = imgFrameData
		frame[1] = gen
		//nolint:gosec // offset < total ≤ 15000, fits uint16.
		binary.LittleEndian.PutUint16(frame[2:4], uint16(offset))
		copy(frame[4:], part)
		frames = append(frames, frame)
	}

	frames = append(frames, []byte{imgFrameCommit, gen})

	return frames, nil
}

// decodeStatus decodes the 3-byte Fan Status characteristic payload: byte 0 is
// the current duty, bytes 1–2 are the measured RPM as a little-endian u16.
// It returns an error if payload is shorter than 3 bytes.
func decodeStatus(payload []byte) (Status, error) {
	if len(payload) < 3 {
		return Status{}, fmt.Errorf("fan status payload too short: got %d bytes, need 3", len(payload))
	}

	duty := int(payload[0])
	rpm := int(binary.LittleEndian.Uint16(payload[1:3]))

	return Status{Duty: duty, RPM: rpm}, nil
}

// decodeEvent decodes the single-byte Fan Event characteristic payload into an
// Event. It returns an error if the payload is empty.
func decodeEvent(payload []byte) (Event, error) {
	if len(payload) < 1 {
		return 0, errors.New("fan event payload empty")
	}

	return Event(payload[0]), nil
}

// decodeCapabilities decodes the 4-byte Fan Capabilities characteristic
// payload: bytes 0–1 are the protocol version, bytes 2–3 are flags, both
// little-endian u16. It returns an error if payload is shorter than 4 bytes.
func decodeCapabilities(payload []byte) (Capabilities, error) {
	if len(payload) < 4 {
		return Capabilities{}, fmt.Errorf("fan capabilities payload too short: got %d bytes, need 4", len(payload))
	}

	version := binary.LittleEndian.Uint16(payload[0:2])
	flags := binary.LittleEndian.Uint16(payload[2:4])

	return Capabilities{ProtocolVersion: version, Flags: flags}, nil
}
