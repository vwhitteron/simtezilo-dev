package fancontroller

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeDevice is an in-memory implementation of the device interface used by
// tests. It records duty writes and control opcodes, lets a test push a Status
// through the stored onStatus callback, and returns canned values for reads.
type fakeDevice struct {
	// caps is returned by ReadCapabilities; defaults to {ProtocolVersion:1}.
	caps Capabilities
	// readStatus is returned by ReadStatus.
	readStatus Status

	mu            sync.Mutex
	onStatus      func(Status)
	onEvent       func(Event)
	dutyWrites    []uint8
	controlOps    []uint8
	displayTexts  []string
	displayImages []displayImage
	discCh        chan struct{}
	closeOnce     sync.Once
}

// displayImage records a single WriteDisplayImage call for assertions.
type displayImage struct {
	pixels        []byte
	width, height int
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{
		caps: Capabilities{ProtocolVersion: 1},
	}
}

func (f *fakeDevice) Connect(_ context.Context, onStatus func(Status), onEvent func(Event)) error {
	f.mu.Lock()
	f.onStatus = onStatus
	f.onEvent = onEvent
	f.discCh = make(chan struct{})
	f.mu.Unlock()

	return nil
}

func (f *fakeDevice) WriteDuty(_ context.Context, duty uint8) error {
	f.mu.Lock()
	f.dutyWrites = append(f.dutyWrites, duty)
	f.mu.Unlock()

	return nil
}

func (f *fakeDevice) ReadStatus(_ context.Context) (Status, error) {
	f.mu.Lock()
	s := f.readStatus
	f.mu.Unlock()

	return s, nil
}

func (f *fakeDevice) ReadCapabilities(_ context.Context) (Capabilities, error) {
	f.mu.Lock()
	caps := f.caps
	f.mu.Unlock()

	return caps, nil
}

func (f *fakeDevice) SendControl(_ context.Context, op uint8) error {
	f.mu.Lock()
	f.controlOps = append(f.controlOps, op)
	f.mu.Unlock()

	return nil
}

func (f *fakeDevice) WriteDisplayText(_ context.Context, text string) error {
	f.mu.Lock()
	f.displayTexts = append(f.displayTexts, text)
	f.mu.Unlock()

	return nil
}

func (f *fakeDevice) WriteDisplayImage(_ context.Context, pixels []byte, width, height int) error {
	f.mu.Lock()

	clone := make([]byte, len(pixels))
	copy(clone, pixels)
	f.displayImages = append(f.displayImages, displayImage{pixels: clone, width: width, height: height})
	f.mu.Unlock()

	return nil
}

func (f *fakeDevice) Disconnected() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.discCh
}

func (f *fakeDevice) Close() error {
	f.closeDisc()

	return nil
}

// closeDisc closes the disconnect channel exactly once, guarded by closeOnce.
func (f *fakeDevice) closeDisc() {
	f.closeOnce.Do(func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		if f.discCh != nil {
			close(f.discCh)
		}
	})
}

// simulateDrop models the BLE link dropping unexpectedly (the watchDisconnect
// path) without a deliberate Close. Sharing closeOnce ensures a later Close
// after a drop will not double-close the channel.
func (f *fakeDevice) simulateDrop() {
	f.closeDisc()
}

// pushStatus delivers a Status to the onStatus callback as if a BLE
// notification had arrived from the firmware.
func (f *fakeDevice) pushStatus(status Status) {
	f.mu.Lock()
	cb := f.onStatus
	f.mu.Unlock()

	if cb != nil {
		cb(status)
	}
}

// pushEvent delivers an Event to the onEvent callback as if a Fan Event
// notification had arrived from the firmware.
func (f *fakeDevice) pushEvent(event Event) {
	f.mu.Lock()
	cb := f.onEvent
	f.mu.Unlock()

	if cb != nil {
		cb(event)
	}
}

// withFakeDevice swaps the package device factory for one returning fake,
// restoring the original when the test ends.
func withFakeDevice(t *testing.T, fake device) {
	t.Helper()

	original := newDevice
	newDevice = func(string) (device, error) {
		return fake, nil
	}

	t.Cleanup(func() {
		newDevice = original
	})
}

// --- behaviour tests ---

func TestSetFanDutyFraming(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	duty, err := client.SetFanDuty(context.Background(), 50)
	require.NoError(t, err)
	require.Equal(t, 50, duty)

	// A repeated duty is cached and not re-sent over the transport.
	_, err = client.SetFanDuty(context.Background(), 50)
	require.NoError(t, err)

	fake.mu.Lock()
	writes := len(fake.dutyWrites)
	fake.mu.Unlock()

	require.Equal(t, 1, writes, "duplicate duty should be cached, not re-sent")
}

func TestGetStatusFraming(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	fake.readStatus = Status{Duty: 50, RPM: 1200}
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	status, err := client.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Duty: 50, RPM: 1200}, status)
}

func TestGetStatusUsesNotifyCache(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	// readStatus differs from the pushed notification to confirm the cache wins.
	fake.readStatus = Status{Duty: 0, RPM: 0}
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	fake.pushStatus(Status{Duty: 75, RPM: 2400})

	status, err := client.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, Status{Duty: 75, RPM: 2400}, status)
}

func TestConnectRequiresAddress(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	withFakeDevice(t, newFakeDevice())

	client := New(Options{CommandTimeout: time.Second})

	require.Error(t, client.Connect(context.Background()))
}

func TestConnectRejectsUnsupportedProtocolVersion(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	for _, version := range []uint16{0, 99} {
		fake := newFakeDevice()
		fake.caps = Capabilities{ProtocolVersion: version}
		withFakeDevice(t, fake)

		client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

		err := client.Connect(context.Background())
		require.Errorf(t, err, "expected error for protocol version %d", version)
	}
}

func TestDisconnectedChannelClosesOnClose(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	disconnected := client.Disconnected()
	require.NotNil(t, disconnected)

	require.NoError(t, client.Close())

	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("disconnected channel not closed after Close")
	}
}

func TestDisconnectedChannelClosesOnUnsolicitedDrop(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	disconnected := client.Disconnected()

	fake.simulateDrop()

	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("disconnected channel not closed after unsolicited drop")
	}
}

func TestUnpairSendsControlOpcode(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.Unpair(context.Background()))

	fake.mu.Lock()
	ops := make([]uint8, len(fake.controlOps))
	copy(ops, fake.controlOps)
	fake.mu.Unlock()

	require.Equal(t, []uint8{0x01}, ops, "Unpair must send the UNPAIR opcode (0x01)")
}

func TestTakeAndReleaseControlSendOpcodes(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.TakeControl(context.Background()))
	require.NoError(t, client.ReleaseControl(context.Background()))

	fake.mu.Lock()
	ops := make([]uint8, len(fake.controlOps))
	copy(ops, fake.controlOps)
	fake.mu.Unlock()

	require.Equal(t, []uint8{0x02, 0x03}, ops,
		"TakeControl/ReleaseControl must send opcodes 0x02 then 0x03")
}

func TestSetDisplayTextWritesAndGuardsLength(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	withFakeDevice(t, fake)

	client := New(Options{Address: "AA:BB:CC:DD:EE:FF", CommandTimeout: time.Second})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.SetDisplayText(context.Background(), "All Vehicles"))

	// A payload longer than the 64-byte limit is rejected before any write.
	err := client.SetDisplayText(context.Background(), strings.Repeat("x", 65))
	require.Error(t, err)

	fake.mu.Lock()
	texts := make([]string, len(fake.displayTexts))
	copy(texts, fake.displayTexts)
	fake.mu.Unlock()

	require.Equal(t, []string{"All Vehicles"}, texts, "over-length text must not be written")
}

func TestOnEventForwardsToOptionsCallback(t *testing.T) { //nolint:paralleltest // mutates the package-global newDevice factory
	fake := newFakeDevice()
	withFakeDevice(t, fake)

	// pushEvent invokes the callback synchronously on this goroutine, so no
	// synchronisation is needed around the recorded slice.
	var events []Event

	client := New(Options{
		Address:        "AA:BB:CC:DD:EE:FF",
		CommandTimeout: time.Second,
		OnEvent: func(e Event) {
			events = append(events, e)
		},
	})

	require.NoError(t, client.Connect(context.Background()))

	t.Cleanup(func() { _ = client.Close() })

	fake.pushEvent(EventButton)
	fake.pushEvent(EventImageOK)

	require.Equal(t, []Event{EventButton, EventImageOK}, events)
}

// --- codec unit tests ---

func TestEncodeDuty(t *testing.T) {
	t.Parallel()

	require.Equal(t, []byte{0}, encodeDuty(0))
	require.Equal(t, []byte{100}, encodeDuty(100))
	require.Equal(t, []byte{50}, encodeDuty(50))
}

func TestDecodeStatus(t *testing.T) {
	t.Parallel()

	t.Run("valid 3-byte payload", func(t *testing.T) {
		t.Parallel()

		// RPM 1200 = 0x04B0 → little-endian bytes [0xB0, 0x04]
		b := []byte{50, 0xB0, 0x04}

		s, err := decodeStatus(b)
		require.NoError(t, err)
		require.Equal(t, Status{Duty: 50, RPM: 1200}, s)
	})

	t.Run("payload longer than 3 bytes is accepted", func(t *testing.T) {
		t.Parallel()

		b := []byte{75, 0xB0, 0x04, 0xFF, 0xFF}

		s, err := decodeStatus(b)
		require.NoError(t, err)
		require.Equal(t, Status{Duty: 75, RPM: 1200}, s)
	})

	t.Run("short buffer returns error", func(t *testing.T) {
		t.Parallel()

		_, err := decodeStatus([]byte{50, 0xB0})
		require.Error(t, err)
	})
}

func TestDecodeEvent(t *testing.T) {
	t.Parallel()

	t.Run("button event", func(t *testing.T) {
		t.Parallel()

		e, err := decodeEvent([]byte{0x01})
		require.NoError(t, err)
		require.Equal(t, EventButton, e)
	})

	t.Run("image-ok event", func(t *testing.T) {
		t.Parallel()

		e, err := decodeEvent([]byte{0x02})
		require.NoError(t, err)
		require.Equal(t, EventImageOK, e)
	})

	t.Run("image-abort event", func(t *testing.T) {
		t.Parallel()

		e, err := decodeEvent([]byte{0x03})
		require.NoError(t, err)
		require.Equal(t, EventImageAbort, e)
	})

	t.Run("empty payload returns error", func(t *testing.T) {
		t.Parallel()

		_, err := decodeEvent([]byte{})
		require.Error(t, err)
	})
}

func TestDecodeCapabilities(t *testing.T) {
	t.Parallel()

	t.Run("valid 4-byte payload", func(t *testing.T) {
		t.Parallel()

		// version=1 (0x0001 LE), flags=0 (0x0000 LE)
		b := []byte{0x01, 0x00, 0x00, 0x00}

		caps, err := decodeCapabilities(b)
		require.NoError(t, err)
		require.Equal(t, Capabilities{ProtocolVersion: 1, Flags: 0}, caps)
	})

	t.Run("payload longer than 4 bytes is accepted", func(t *testing.T) {
		t.Parallel()

		b := []byte{0x01, 0x00, 0x03, 0x00, 0xFF}

		caps, err := decodeCapabilities(b)
		require.NoError(t, err)
		require.Equal(t, Capabilities{ProtocolVersion: 1, Flags: 3}, caps)
	})

	t.Run("short buffer returns error", func(t *testing.T) {
		t.Parallel()

		_, err := decodeCapabilities([]byte{0x01, 0x00, 0x00})
		require.Error(t, err)
	})
}
