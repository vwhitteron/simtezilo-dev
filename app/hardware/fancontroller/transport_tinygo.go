package fancontroller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

const (
	// Fan GATT profile UUIDs. All multi-byte integers in characteristic
	// payloads are little-endian. These must match the firmware and
	// docs/fan-gatt-profile.md exactly.
	fanServiceUUID      = "7a3e0001-87d1-3091-411d-000002373705"
	fanDutyUUID         = "7a3e0002-87d1-3091-411d-000002373705"
	fanStatusUUID       = "7a3e0003-87d1-3091-411d-000002373705"
	fanCapabilitiesUUID = "7a3e0004-87d1-3091-411d-000002373705"
	fanControlUUID      = "7a3e0005-87d1-3091-411d-000002373705"
	fanEventUUID        = "7a3e0006-87d1-3091-411d-000002373705"
	fanDisplayTextUUID  = "7a3e0007-87d1-3091-411d-000002373705"
	fanDisplayImageUUID = "7a3e0008-87d1-3091-411d-000002373705"

	// connectionPollInterval is how often the disconnect watcher polls the
	// device connection state. tinygo's connect handler is not guaranteed to
	// fire on an unsolicited drop in central mode, so polling the Connected
	// property is the reliable signal.
	connectionPollInterval = time.Second
)

// enableAdapterOnce enables the default BLE adapter exactly once for the
// process. The adapter is shared across reconnect attempts.
var enableAdapterOnce = sync.OnceValue(func() error { //nolint:gochecknoglobals
	return bluetooth.DefaultAdapter.Enable()
})

// mustUUID parses a compile-time-constant UUID, panicking on a malformed
// constant (a programming error, never a runtime condition).
func mustUUID(s string) bluetooth.UUID {
	uuid, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic("fancontroller: invalid UUID constant " + s + ": " + err.Error())
	}

	return uuid
}

// newDevice builds the default typed GATT device for a device address. It is a
// var so tests can inject a fake device without a real BLE adapter.
var newDevice = func(address string) (device, error) { //nolint:gochecknoglobals
	return &bleDevice{
		address:          address,
		serviceUUID:      mustUUID(fanServiceUUID),
		dutyUUID:         mustUUID(fanDutyUUID),
		statusUUID:       mustUUID(fanStatusUUID),
		capabilitiesUUID: mustUUID(fanCapabilitiesUUID),
		controlUUID:      mustUUID(fanControlUUID),
		eventUUID:        mustUUID(fanEventUUID),
		displayTextUUID:  mustUUID(fanDisplayTextUUID),
		displayImageUUID: mustUUID(fanDisplayImageUUID),
	}, nil
}

// bleDevice speaks the fan GATT profile over tinygo's cross-platform BLE stack
// (Linux=BlueZ, macOS=CoreBluetooth, Windows=WinRT). The device must already
// be paired via the Bluetooth panel; the transport connects by address.
type bleDevice struct {
	address          string
	serviceUUID      bluetooth.UUID
	dutyUUID         bluetooth.UUID
	statusUUID       bluetooth.UUID
	capabilitiesUUID bluetooth.UUID
	controlUUID      bluetooth.UUID
	eventUUID        bluetooth.UUID
	displayTextUUID  bluetooth.UUID
	displayImageUUID bluetooth.UUID

	mu                   sync.Mutex
	btDevice             bluetooth.Device
	dutyChar             bluetooth.DeviceCharacteristic
	statusChar           bluetooth.DeviceCharacteristic
	capsChar             bluetooth.DeviceCharacteristic
	controlChar          bluetooth.DeviceCharacteristic
	eventChar            bluetooth.DeviceCharacteristic
	displayTextChar      bluetooth.DeviceCharacteristic
	displayImageChar     bluetooth.DeviceCharacteristic
	haveEvent            bool
	haveDisplayText      bool
	haveDisplayImage     bool
	imageGen             uint8
	notificationsEnabled bool
	connected            bool
	closed               bool
	discCh               chan struct{}
	stopCh               chan struct{}
}

// Connect opens the BLE link, resolves all fan GATT characteristics, enables
// notifications on Fan Status and Fan Event, and starts the disconnect watcher.
// onStatus and onEvent are invoked from tinygo's notification goroutine and must
// not block.
func (d *bleDevice) Connect(_ context.Context, onStatus func(Status), onEvent func(Event)) error {
	err := enableAdapterOnce()
	if err != nil {
		return fmt.Errorf("enable BLE adapter: %w", err)
	}

	var addr bluetooth.Address
	addr.Set(d.address)

	btDev, err := bluetooth.DefaultAdapter.Connect(addr, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect to %s: %w", d.address, err)
	}

	err = d.resolveCharacteristics(btDev, onStatus, onEvent)
	if err != nil {
		_ = btDev.Disconnect()

		return err
	}

	d.mu.Lock()
	d.btDevice = btDev
	d.connected = true
	d.discCh = make(chan struct{})
	d.stopCh = make(chan struct{})
	stopCh := d.stopCh
	discCh := d.discCh
	d.mu.Unlock()

	go d.watchDisconnect(btDev, stopCh, discCh)

	return nil
}

// WriteDuty writes the duty byte to the Fan Duty characteristic using
// write-without-response; the latest write wins with no ATT acknowledgement.
func (d *bleDevice) WriteDuty(_ context.Context, duty uint8) error {
	d.mu.Lock()
	dutyChar := d.dutyChar
	connected := d.connected
	d.mu.Unlock()

	if !connected {
		return errNotConnected
	}

	_, err := dutyChar.WriteWithoutResponse(encodeDuty(duty))
	if err != nil {
		return fmt.Errorf("write Fan Duty characteristic: %w", err)
	}

	return nil
}

// ReadStatus reads the Fan Status characteristic and decodes the 3-byte
// payload into a Status.
func (d *bleDevice) ReadStatus(_ context.Context) (Status, error) {
	d.mu.Lock()
	statusChar := d.statusChar
	connected := d.connected
	d.mu.Unlock()

	if !connected {
		return Status{}, errNotConnected
	}

	buf := make([]byte, 8)

	n, err := statusChar.Read(buf)
	if err != nil {
		return Status{}, fmt.Errorf("read Fan Status characteristic: %w", err)
	}

	return decodeStatus(buf[:n])
}

// ReadCapabilities reads the Fan Capabilities characteristic and decodes the
// 4-byte payload into a Capabilities.
func (d *bleDevice) ReadCapabilities(_ context.Context) (Capabilities, error) {
	d.mu.Lock()
	capsChar := d.capsChar
	connected := d.connected
	d.mu.Unlock()

	if !connected {
		return Capabilities{}, errNotConnected
	}

	buf := make([]byte, 8)

	n, err := capsChar.Read(buf)
	if err != nil {
		return Capabilities{}, fmt.Errorf("read Fan Capabilities characteristic: %w", err)
	}

	return decodeCapabilities(buf[:n])
}

// SendControl writes a single-byte opcode to the Fan Control characteristic.
func (d *bleDevice) SendControl(_ context.Context, opcode uint8) error {
	d.mu.Lock()
	controlChar := d.controlChar
	connected := d.connected
	d.mu.Unlock()

	if !connected {
		return errNotConnected
	}

	_, err := controlChar.WriteWithoutResponse([]byte{opcode})
	if err != nil {
		return fmt.Errorf("write Fan Control characteristic: %w", err)
	}

	return nil
}

// WriteDisplayText writes a UTF-8 string to the Display Text characteristic using
// write-without-response, the same as Fan Duty and Fan Control. A write-with-
// response stalls because the firmware does not complete the ATT write response,
// which leaves the characteristic's write pending in BlueZ and makes every
// subsequent write fail with org.bluez.Error.InProgress; a command write needs
// no acknowledgement and avoids that. It is a no-op error if the device does not
// expose the characteristic.
func (d *bleDevice) WriteDisplayText(_ context.Context, text string) error {
	d.mu.Lock()
	displayTextChar := d.displayTextChar
	have := d.haveDisplayText
	connected := d.connected
	d.mu.Unlock()

	if !connected {
		return errNotConnected
	}

	if !have {
		return errors.New("device does not support Display Text")
	}

	_, err := displayTextChar.WriteWithoutResponse([]byte(text))
	if err != nil {
		return fmt.Errorf("write Display Text characteristic: %w", err)
	}

	return nil
}

// WriteDisplayImage streams a little-endian RGB565 icon to the Display Image
// characteristic as a BEGIN/DATA…/COMMIT frame sequence. Every frame, including
// BEGIN, is written Write-Without-Response, the same as Fan Duty, Fan Control and
// Display Text: this firmware does not complete the ATT write response, so a
// write-with-response stalls in BlueZ (and can leave the characteristic pending,
// failing later writes with org.bluez.Error.InProgress). The device is pre-paired
// via the OS, and sequential command writes to one characteristic are delivered
// in order, so BEGIN needs neither an acknowledgement nor a response-ordered
// barrier. The transfer honours ctx: a cancelled write (a newer icon superseded
// this one) stops before COMMIT so the device discards the partial. It is a no-op
// error if the device does not expose the characteristic.
func (d *bleDevice) WriteDisplayImage(ctx context.Context, pixels []byte, width, height int) error {
	d.mu.Lock()
	displayImageChar := d.displayImageChar
	have := d.haveDisplayImage
	connected := d.connected
	gen := d.imageGen
	d.imageGen++
	d.mu.Unlock()

	if !connected {
		return errNotConnected
	}

	if !have {
		return errors.New("device does not support Display Image")
	}

	// Size DATA frames to the negotiated MTU when available, capped at the
	// default. GetMTU reports a write-value length (darwin) or the ATT MTU
	// (linux); subtracting the ATT (3) and frame (4) headers is safe for both.
	chunk := imgDefaultChunk

	mtu, mtuErr := displayImageChar.GetMTU()
	if mtuErr == nil {
		if usable := int(mtu) - 3 - 4; usable >= 1 && usable < chunk {
			chunk = usable
		}
	}

	frames, err := encodeImageFrames(pixels, width, height, gen, chunk)
	if err != nil {
		return err
	}

	for idx, frame := range frames {
		// A cancelled transfer (e.g. a newer icon supersedes this one) stops here
		// without sending COMMIT, so the device discards the partial; the next
		// transfer uses a fresh generation that the device accepts cleanly.
		err := ctx.Err()
		if err != nil {
			return err
		}

		_, werr := displayImageChar.WriteWithoutResponse(frame)
		if werr != nil {
			return fmt.Errorf("write Display Image frame %d/%d: %w", idx+1, len(frames), werr)
		}
	}

	return nil
}

// Disconnected returns a channel closed when the underlying BLE link drops.
func (d *bleDevice) Disconnected() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.discCh
}

// Close tears down the link and stops the disconnect watcher. It is idempotent.
func (d *bleDevice) Close() error {
	d.mu.Lock()

	if d.closed {
		d.mu.Unlock()

		return nil
	}

	d.closed = true
	connected := d.connected
	d.connected = false
	btDev := d.btDevice
	stopCh := d.stopCh
	d.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}

	// Unsubscribe before disconnecting (and even on an already-dropped link) so
	// the underlying notification machinery is torn down rather than leaking. On
	// BlueZ each EnableNotifications registers a process-global D-Bus signal
	// watcher + match rule that only EnableNotifications(nil) removes; without
	// this, every reconnect leaves a stale watcher that still fires on the
	// (stable) characteristic path, so one device notification is delivered once
	// per past connection.
	d.disableNotifications()

	if !connected {
		return nil
	}

	err := btDev.Disconnect()
	if err != nil {
		return fmt.Errorf("disconnect device: %w", err)
	}

	return nil
}

// disableNotifications tears down the Fan Status and Fan Event subscriptions. It
// is a no-op if notifications were never enabled (e.g. connect failed before
// resolving characteristics), which also avoids calling into an unresolved
// characteristic. A failed unsubscribe (e.g. the link already dropped) still
// releases the local D-Bus watcher, so it is best-effort.
func (d *bleDevice) disableNotifications() {
	d.mu.Lock()
	enabled := d.notificationsEnabled
	d.notificationsEnabled = false
	statusChar := d.statusChar
	eventChar := d.eventChar
	haveEvent := d.haveEvent
	d.mu.Unlock()

	if !enabled {
		return
	}

	_ = statusChar.EnableNotifications(nil)

	if haveEvent {
		_ = eventChar.EnableNotifications(nil)
	}
}

// resolveCharacteristics discovers the fan GATT service and its characteristics,
// enabling notifications on Fan Status. It returns an error if any required
// characteristic is absent.
func (d *bleDevice) resolveCharacteristics(
	btDev bluetooth.Device,
	onStatus func(Status),
	onEvent func(Event),
) error {
	services, err := btDev.DiscoverServices([]bluetooth.UUID{d.serviceUUID})
	if err != nil {
		return fmt.Errorf("discover fan service: %w", err)
	}

	if len(services) == 0 {
		return errors.New("fan service not found on device")
	}

	chars, err := services[0].DiscoverCharacteristics([]bluetooth.UUID{
		d.dutyUUID,
		d.statusUUID,
		d.capabilitiesUUID,
		d.controlUUID,
		d.eventUUID,
		d.displayTextUUID,
		d.displayImageUUID,
	})
	if err != nil {
		return fmt.Errorf("discover fan characteristics: %w", err)
	}

	err = d.assignCharacteristics(chars)
	if err != nil {
		return err
	}

	err = d.enableNotifications(onStatus, onEvent)
	if err != nil {
		return err
	}

	d.notificationsEnabled = true

	return nil
}

// enableNotifications subscribes to Fan Status (always) and Fan Event (when the
// device exposes it and a callback is supplied). Malformed payloads are dropped.
func (d *bleDevice) enableNotifications(onStatus func(Status), onEvent func(Event)) error {
	err := d.statusChar.EnableNotifications(func(payload []byte) {
		status, decodeErr := decodeStatus(payload)
		if decodeErr != nil {
			// Malformed payload — ignore and wait for the next notification.
			return
		}

		onStatus(status)
	})
	if err != nil {
		return fmt.Errorf("enable Fan Status notifications: %w", err)
	}

	if !d.haveEvent || onEvent == nil {
		return nil
	}

	err = d.eventChar.EnableNotifications(func(payload []byte) {
		event, decodeErr := decodeEvent(payload)
		if decodeErr != nil {
			// Malformed payload — ignore and wait for the next notification.
			return
		}

		onEvent(event)
	})
	if err != nil {
		return fmt.Errorf("enable Fan Event notifications: %w", err)
	}

	return nil
}

// assignCharacteristics walks the discovered characteristic list and stores each
// recognised UUID on the receiver. Duty, Status, Capabilities and Control are
// required; the function returns an error if any is missing. Event, Display Text
// and Display Image are optional (additive within protocol v1): a device that
// predates them still connects, with those features disabled.
func (d *bleDevice) assignCharacteristics(chars []bluetooth.DeviceCharacteristic) error {
	required := map[bluetooth.UUID]*bluetooth.DeviceCharacteristic{
		d.dutyUUID:         &d.dutyChar,
		d.statusUUID:       &d.statusChar,
		d.capabilitiesUUID: &d.capsChar,
		d.controlUUID:      &d.controlChar,
	}

	var found int

	for idx := range chars {
		switch chars[idx].UUID() {
		case d.eventUUID:
			d.eventChar = chars[idx]
			d.haveEvent = true
		case d.displayTextUUID:
			d.displayTextChar = chars[idx]
			d.haveDisplayText = true
		case d.displayImageUUID:
			d.displayImageChar = chars[idx]
			d.haveDisplayImage = true
		default:
			if ptr, ok := required[chars[idx].UUID()]; ok {
				*ptr = chars[idx]
				found++
			}
		}
	}

	if found != len(required) {
		return fmt.Errorf(
			"required fan characteristics not found: matched %d of %d", found, len(required))
	}

	return nil
}

// watchDisconnect polls the device connection state and closes discCh when the
// link drops, so the control loop can reconnect. It exits when stopCh is closed
// (deliberate Close) or once a drop has been signalled.
func (d *bleDevice) watchDisconnect(btDev bluetooth.Device, stopCh, discCh chan struct{}) {
	ticker := time.NewTicker(connectionPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			connected, err := btDev.Connected()
			if err == nil && connected {
				continue
			}

			d.mu.Lock()
			stillConnected := d.connected
			d.connected = false
			d.mu.Unlock()

			if stillConnected {
				close(discCh)
			}

			return
		}
	}
}
