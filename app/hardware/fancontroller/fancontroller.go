package fancontroller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-ble/ble"
)

const (
	defaultDeviceName = "windsim"
	defaultTimeout    = 5 * time.Second
	keepaliveInterval = 3 * time.Second

	// Nordic UART Service UUIDs used by the wind simulator firmware.
	nusRXUUID = "6e400002-b5a3-f393-e0a9-e50e24dcca9e"
	nusTXUUID = "6e400003-b5a3-f393-e0a9-e50e24dcca9e"
)

var errNotConnected = errors.New("windsim not connected")

// Status is returned by the STATUS command.
type Status struct {
	Duty int
	RPM  int
}

// Options configures a wind simulator client.
type Options struct {
	DeviceName     string
	CommandTimeout time.Duration
}

// Client handles BLE communication with a wind simulator device.
type Client struct {
	options Options

	mu              sync.RWMutex
	bleClient       ble.Client
	rxChar          *ble.Characteristic
	txChar          *ble.Characteristic
	notifyBuf       bytes.Buffer
	lineCh          chan string
	commandMu       sync.Mutex
	connected       bool
	deviceName      string
	lastDuty        int
	keepaliveCancel context.CancelFunc
}

// New creates a wind simulator BLE client.
func New(options Options) *Client {
	if options.DeviceName == "" {
		options.DeviceName = defaultDeviceName
	}

	if options.CommandTimeout <= 0 {
		options.CommandTimeout = defaultTimeout
	}

	return &Client{
		options:    options,
		lineCh:     make(chan string, 64),
		deviceName: "",
		lastDuty:   -1,
	}
}

// Connect scans for a windsim BLE device and subscribes to UART TX notifications.
func (c *Client) Connect(ctx context.Context) error {
	err := setupDefaultBLEDevice()
	if err != nil {
		return fmt.Errorf("setup default ble device: %w", err)
	}

	deviceName := strings.ToLower(strings.TrimSpace(c.options.DeviceName))

	address, err := c.scanForAddress(ctx, deviceName)
	if err != nil {
		return err
	}

	bleClient, err := ble.Dial(ctx, ble.NewAddr(address))
	if err != nil {
		return fmt.Errorf("dial windsim device: %w", err)
	}

	profile, err := bleClient.DiscoverProfile(true)
	if err != nil {
		_ = bleClient.CancelConnection()

		return fmt.Errorf("discover profile: %w", err)
	}

	rxCharacteristic, txCharacteristic, err := findNUSCharacteristics(profile)
	if err != nil {
		_ = bleClient.CancelConnection()

		return err
	}

	err = bleClient.Subscribe(txCharacteristic, false, c.handleNotification)
	if err != nil {
		_ = bleClient.CancelConnection()

		return fmt.Errorf("subscribe to tx characteristic: %w", err)
	}

	keepaliveCtx, keepaliveCancel := context.WithCancel(context.Background())

	c.mu.Lock()
	c.bleClient = bleClient
	c.rxChar = rxCharacteristic
	c.txChar = txCharacteristic
	c.connected = true
	c.notifyBuf.Reset()
	c.deviceName = deviceName
	c.lastDuty = -1
	c.keepaliveCancel = keepaliveCancel
	c.mu.Unlock()

	go c.runKeepalive(keepaliveCtx) //nolint:contextcheck // intentionally independent of connect context

	return nil
}

// Close ends the active connection.
func (c *Client) Close() error {
	c.mu.RLock()
	bleClient := c.bleClient
	connected := c.connected
	keepaliveCancel := c.keepaliveCancel
	c.mu.RUnlock()

	if !connected || bleClient == nil {
		return nil
	}

	if keepaliveCancel != nil {
		keepaliveCancel()
	}

	err := bleClient.CancelConnection()

	c.mu.Lock()
	c.connected = false
	c.bleClient = nil
	c.rxChar = nil
	c.txChar = nil
	c.notifyBuf.Reset()
	c.keepaliveCancel = nil
	c.mu.Unlock()

	if err != nil {
		return fmt.Errorf("cancel connection: %w", err)
	}

	return nil
}

// Ping sends PING and expects PONG.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "PING", func(line string) bool {
		return line == "PONG"
	})
	if err != nil {
		return err
	}

	return nil
}

// Unpair sends UNPAIR and expects OK:UNPAIR.
func (c *Client) Unpair(ctx context.Context) error {
	_, err := c.sendCommand(ctx, "UNPAIR", func(line string) bool {
		return line == "OK:UNPAIR"
	})
	if err != nil {
		return err
	}

	return nil
}

// SetFanDuty sends FAN:<0-100> and returns the acknowledged duty.
// If the duty has not changed since the last successful call, the BLE
// command is skipped and the cached value is returned.
func (c *Client) SetFanDuty(ctx context.Context, duty int) (int, error) {
	if duty < 0 || duty > 100 {
		return 0, fmt.Errorf("invalid duty %d: must be in range 0-100", duty)
	}

	if duty == c.lastDuty {
		return duty, nil
	}

	line, err := c.sendCommand(ctx, fmt.Sprintf("FAN:%d", duty), func(resp string) bool {
		return strings.HasPrefix(resp, "FAN:")
	})
	if err != nil {
		return 0, err
	}

	ackDuty, err := parseFanResponse(line)
	if err != nil {
		return 0, err
	}

	c.lastDuty = ackDuty

	return ackDuty, nil
}

// GetStatus sends STATUS and returns parsed duty and RPM values.
func (c *Client) GetStatus(ctx context.Context) (Status, error) {
	line, err := c.sendCommand(ctx, "STATUS", func(resp string) bool {
		return strings.HasPrefix(resp, "STATUS:")
	})
	if err != nil {
		return Status{}, err
	}

	status, err := parseStatusResponse(line)
	if err != nil {
		return Status{}, err
	}

	return status, nil
}

func (c *Client) DeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.deviceName
}

// Disconnected returns a channel that is closed when the BLE connection drops.
// Returns nil if not connected.
func (c *Client) Disconnected() <-chan struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.bleClient == nil {
		return nil
	}

	return c.bleClient.Disconnected()
}

// runKeepalive sends periodic PING commands to keep the BLE connection alive.
// It stops when the context is cancelled (e.g. on Close).
func (c *Client) runKeepalive(ctx context.Context) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.Ping(ctx)
		}
	}
}

func (c *Client) scanForAddress(ctx context.Context, deviceName string) (string, error) {
	addressCh := make(chan string, 1)
	scanCtx, cancelScan := context.WithCancel(ctx)

	err := ble.Scan(scanCtx, false, func(advertisement ble.Advertisement) {
		name := strings.ToLower(strings.TrimSpace(advertisement.LocalName()))
		if name != deviceName {
			return
		}

		select {
		case addressCh <- advertisement.Addr().String():
			cancelScan()
		default:
		}
	}, nil)

	cancelScan()

	if !isBenignScanError(err) {
		return "", fmt.Errorf("scan windsim device: %w", err)
	}

	select {
	case address := <-addressCh:
		return address, nil
	default:
		contextErr := scanContextError(ctx)
		if contextErr != nil {
			return "", contextErr
		}

		return "", errors.New("scan windsim device: matching device not found")
	}
}

func isBenignScanError(err error) bool {
	if err == nil {
		return true
	}

	if errors.Is(err, context.Canceled) {
		return true
	}

	return errors.Is(err, context.DeadlineExceeded)
}

func scanContextError(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}

	return fmt.Errorf("scan windsim device: %w", ctx.Err())
}

func (c *Client) sendCommand(ctx context.Context, command string, matcher func(string) bool) (string, error) {
	if matcher == nil {
		return "", errors.New("matcher is required")
	}

	c.commandMu.Lock()
	defer c.commandMu.Unlock()

	err := c.writeLine(command)
	if err != nil {
		return "", err
	}

	responseCtx := ctx

	if c.options.CommandTimeout > 0 {
		var cancel context.CancelFunc

		responseCtx, cancel = context.WithTimeout(ctx, c.options.CommandTimeout)
		defer cancel()
	}

	for {
		select {
		case <-responseCtx.Done():
			return "", fmt.Errorf("wait response for %q: %w", command, responseCtx.Err())
		case line := <-c.lineCh:
			if matcher(line) {
				return line, nil
			}
		}
	}
}

func (c *Client) writeLine(command string) error {
	c.mu.RLock()
	bleClient := c.bleClient
	rxCharacteristic := c.rxChar
	connected := c.connected
	c.mu.RUnlock()

	if !connected || bleClient == nil || rxCharacteristic == nil {
		return errNotConnected
	}

	payload := []byte(strings.TrimSpace(command) + "\n")

	err := bleClient.WriteCharacteristic(rxCharacteristic, payload, false)
	if err != nil {
		return fmt.Errorf("write command %q: %w", command, err)
	}

	return nil
}

func (c *Client) handleNotification(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, _ = c.notifyBuf.Write(data)

	for {
		buffer := c.notifyBuf.Bytes()

		index := bytes.IndexByte(buffer, '\n')
		if index < 0 {
			return
		}

		line := strings.TrimSpace(string(buffer[:index]))
		c.notifyBuf.Next(index + 1)

		if line == "" {
			continue
		}

		select {
		case c.lineCh <- line:
		default:
			// Drop oldest stale response to avoid blocking notification handler.
			<-c.lineCh

			c.lineCh <- line
		}
	}
}

func findNUSCharacteristics(profile *ble.Profile) (*ble.Characteristic, *ble.Characteristic, error) {
	if profile == nil {
		return nil, nil, errors.New("nil profile")
	}

	rxUUID := ble.MustParse(nusRXUUID)
	txUUID := ble.MustParse(nusTXUUID)

	var (
		rxChar *ble.Characteristic
		txChar *ble.Characteristic
	)

	for _, service := range profile.Services {
		for _, characteristic := range service.Characteristics {
			if characteristic.UUID.Equal(rxUUID) {
				rxChar = characteristic
			}

			if characteristic.UUID.Equal(txUUID) {
				txChar = characteristic
			}
		}
	}

	if rxChar == nil || txChar == nil {
		return nil, nil, fmt.Errorf("required NUS characteristics not found (rx=%t tx=%t)", rxChar != nil, txChar != nil)
	}

	return rxChar, txChar, nil
}

func parseFanResponse(line string) (int, error) {
	if !strings.HasPrefix(line, "FAN:") {
		return 0, fmt.Errorf("invalid FAN response: %q", line)
	}

	dutyText := strings.TrimSpace(strings.TrimPrefix(line, "FAN:"))

	duty, err := strconv.Atoi(dutyText)
	if err != nil {
		return 0, fmt.Errorf("parse FAN duty %q: %w", dutyText, err)
	}

	if duty < 0 || duty > 100 {
		return 0, fmt.Errorf("invalid FAN duty in response: %d", duty)
	}

	return duty, nil
}

func parseStatusResponse(line string) (Status, error) {
	if !strings.HasPrefix(line, "STATUS:") {
		return Status{}, fmt.Errorf("invalid STATUS response: %q", line)
	}

	payload := strings.TrimPrefix(line, "STATUS:")

	parts := strings.Split(payload, ",")
	if len(parts) != 2 {
		return Status{}, fmt.Errorf("invalid STATUS payload: %q", payload)
	}

	dutyPart := strings.TrimSpace(parts[0])
	rpmPart := strings.TrimSpace(parts[1])

	if !strings.HasPrefix(dutyPart, "DUTY:") {
		return Status{}, fmt.Errorf("invalid STATUS duty field: %q", dutyPart)
	}

	if !strings.HasPrefix(rpmPart, "RPM:") {
		return Status{}, fmt.Errorf("invalid STATUS rpm field: %q", rpmPart)
	}

	dutyText := strings.TrimPrefix(dutyPart, "DUTY:")
	rpmText := strings.TrimPrefix(rpmPart, "RPM:")

	duty, err := strconv.Atoi(dutyText)
	if err != nil {
		return Status{}, fmt.Errorf("parse STATUS duty %q: %w", dutyText, err)
	}

	rpm, err := strconv.Atoi(rpmText)
	if err != nil {
		return Status{}, fmt.Errorf("parse STATUS rpm %q: %w", rpmText, err)
	}

	if duty < 0 || duty > 100 {
		return Status{}, fmt.Errorf("invalid STATUS duty value: %d", duty)
	}

	if rpm < 0 {
		return Status{}, fmt.Errorf("invalid STATUS rpm value: %d", rpm)
	}

	return Status{Duty: duty, RPM: rpm}, nil
}
