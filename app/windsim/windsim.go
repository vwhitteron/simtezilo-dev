// Package windsim implements the wind-simulator fan-control subsystem.
// It maintains a BLE connection to the fan controller device and drives the
// fan duty cycle in proportion to the vehicle's ground speed.
package windsim

import (
	"context"
	"image/color"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/hardware/fancontroller"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/icons"
)

// Telemetry is the subset of gt-telemetry the wind simulator reads.
type Telemetry interface {
	GroundSpeedKPH() float32
	TelemetryStarted() bool
	IsOnCircuit() bool
	VehicleHasOpenCockpit() bool
}

// fanClient is the subset of *fancontroller.Client used by the controller,
// extracted so the control loop can be tested with a fake.
type fanClient interface {
	Connect(ctx context.Context) error
	Close() error
	DeviceAddress() string
	SetFanDuty(ctx context.Context, duty int) (int, error)
	TakeControl(ctx context.Context) error
	ReleaseControl(ctx context.Context) error
	SetDisplayImage(ctx context.Context, pixels []byte, width, height int) error
	Disconnected() <-chan struct{}
}

// fanCommand instructs the duty-cycle loop whether the app should actively drive
// the fan this tick. When drive is false the app holds no control and the device
// controls the fan locally via its rotary encoder; duty is only meaningful when
// drive is true.
type fanCommand struct {
	drive bool
	duty  int
}

const (
	fanConnectTimeout = 30 * time.Second
	fanReconnectDelay = 10 * time.Second
	fanModeCheckDelay = 5 * time.Second
	// fanConfigCheckInterval is how often the active duty loop re-reads config to
	// apply enable/disable and device-selection changes live. The check is just a
	// few mutex-guarded field reads (no I/O), so a tight interval is cheap.
	fanConfigCheckInterval = 1 * time.Second
	fanShutdownTimeout     = 2 * time.Second
	fanMaxSpeedKPH         = 250.0
	// fanDisplayUploadTimeout bounds a single Display Image transfer so a wedged
	// link cannot stall the uploader goroutine indefinitely.
	fanDisplayUploadTimeout = 5 * time.Second
)

// Controller owns all wind-simulator state and the BLE fan-controller connection.
type Controller struct {
	config *config.Config
	log    zerolog.Logger
	ctx    context.Context //nolint:containedctx // Context for managing lifecycle
	wg     *sync.WaitGroup

	// Lazy host accessors (evaluated at call time, matching the original
	// a.gtClient.Telemetry / a.telemetryIsActive() / a.state.isInPostRaceMenu reads).
	telemetry       func() Telemetry
	telemetryActive func() bool
	inPostRaceMenu  func() bool

	controlChan chan fanCommand          // was fanControlChan
	eventChan   chan fancontroller.Event // was fanEventChan
	client      fanClient                // was fanController
}

// NewController creates a Controller. telemetry, telemetryActive and inPostRaceMenu
// are lazy accessors evaluated at call time so the Controller never holds a
// direct pointer into the App struct.
func NewController(
	cfg *config.Config,
	log zerolog.Logger,
	ctx context.Context,
	wg *sync.WaitGroup,
	telemetry func() Telemetry,
	telemetryActive func() bool,
	inPostRaceMenu func() bool,
) *Controller {
	return &Controller{
		config:          cfg,
		log:             log,
		ctx:             ctx,
		wg:              wg,
		telemetry:       telemetry,
		telemetryActive: telemetryActive,
		inPostRaceMenu:  inPostRaceMenu,
	}
}

// Initialize sets up the fan controller channel and logs the configured mode.
// The BLE goroutine is always started and waits for the mode to become active.
func (c *Controller) Initialize() {
	if !c.config.IsFanModeValid() {
		c.log.Warn().
			Str("component", "fan").
			Str("configuredMode", c.config.GetFanConfiguredMode()).
			Str("fallbackMode", "manual").
			Msg("Invalid fan mode configured, falling back to manual")
	}

	c.controlChan = make(chan fanCommand, 1)
	c.eventChan = make(chan fancontroller.Event, 8)

	c.log.Info().
		Str("component", "fan").
		Str("mode", c.config.GetFanMode()).
		Str("address", c.config.GetFanDeviceAddress()).
		Msg("Initialized")
}

// StartTask starts the wind simulator background goroutine.
func (c *Controller) StartTask() {
	c.wg.Add(1)

	go func() {
		defer func() {
			c.log.Debug().Msg("Fan control goroutine exiting")
			c.wg.Done()
		}()

		c.runFanControlTask()
	}()
}

// Close cleanly closes the wind simulator client connection.
func (c *Controller) Close() {
	if c.client == nil {
		return
	}

	c.log.Info().Msg("Closing fan controller client")

	err := c.client.Close()
	if err != nil {
		c.log.Error().
			Err(err).
			Str("component", "fan").
			Str("result", "failure").
			Msg("Close")
	}

	c.log.Info().Msg("Fan controller close phase complete")
}

// runFanControlTask connects to the fan device and maintains the output duty cycle, reconnecting on errors.
// When mode is "manual" it waits, polling periodically for the mode to change.
func (c *Controller) runFanControlTask() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if c.fanShouldIdle() {
			if c.fanWait(fanModeCheckDelay) {
				return
			}

			continue
		}

		c.log.Info().Str("component", "fan").Msg("Scanning for device")

		client := c.createFanControllerClient()

		connectCtx, connectCancel := context.WithTimeout(c.ctx, fanConnectTimeout)
		err := client.Connect(connectCtx)

		connectCancel()

		if err != nil {
			c.log.Warn().
				Err(err).
				Str("component", "fan").
				Msg("Connect failed, retrying")

			if c.fanWait(fanReconnectDelay) {
				return
			}

			continue
		}

		c.client = client

		c.log.Info().Str("component", "fan").Str("address", client.DeviceAddress()).Msg("Connected")

		reconnect := c.runFanControlDutyCycle()

		c.client = nil

		_ = client.Close()

		if !reconnect {
			return
		}

		c.log.Info().Str("component", "fan").Msg("Connection lost, reconnecting")

		if c.fanWait(fanReconnectDelay) {
			return
		}
	}
}

// fanWait sleeps for delay or until the controller context is cancelled. It returns true
// if the context was cancelled (the caller should stop), false if the delay
// elapsed normally.
func (c *Controller) fanWait(delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return false
	case <-c.ctx.Done():
		return true
	}
}

// fanShouldIdle reports whether the fan control task should stay idle rather than
// hold a connection. The link is kept up whenever the fan is enabled and an
// address is set, regardless of mode: the device's button (mode cycling) and
// screen (mode display) need a live link even in manual mode, when the app never
// drives the fan itself.
func (c *Controller) fanShouldIdle() bool {
	return !c.config.GetExperimentalFeaturesEnabled() ||
		!c.config.FanEnabled() ||
		c.config.GetFanDeviceAddress() == ""
}

// createFanControllerClient builds a new fancontroller.Client from the current config.
func (c *Controller) createFanControllerClient() *fancontroller.Client {
	cmdTimeout := time.Duration(c.config.GetFanCommandTimeoutMs()) * time.Millisecond

	return fancontroller.New(fancontroller.Options{
		Address:        c.config.GetFanDeviceAddress(),
		CommandTimeout: cmdTimeout,
		// OnEvent runs on the BLE notification goroutine and must not block, so it
		// hands the event to the duty-cycle loop via a buffered channel. A full
		// channel drops the event rather than stalling the BLE stack.
		OnEvent: func(e fancontroller.Event) {
			select {
			case c.eventChan <- e:
			default:
			}
		},
	})
}

// fanControlState tracks the app's control arbitration with the device across one
// connection. controlHeld is true while the app holds a TAKE_CONTROL override and
// is driving Fan Duty. The device locks the encoder out while the app holds
// control, so the app only relinquishes via RELEASE_CONTROL (when it goes idle)
// or a disconnect — there is no device-initiated control-release event.
type fanControlState struct {
	controlHeld bool
	// display queues mode-icon uploads to the per-connection uploader goroutine,
	// so the control loop never blocks on the multi-frame BLE transfer. The
	// uploader preempts an in-flight upload when a newer icon is queued.
	display chan fanDisplayJob
}

// fanDisplayJob is one request to show a mode icon on the device screen: the SVG
// icon name and the foreground colour to render it in.
type fanDisplayJob struct {
	name       string
	foreground color.Color
}

// runFanControlDutyCycle drives the fan only while wind simulation is active,
// bracketing those periods with TAKE_CONTROL / RELEASE_CONTROL so the device
// restores the user's manual (encoder) setpoint when the app is not driving. It
// also reacts to Fan Events: a front-button press cycles the fan mode (reflected
// back to the device screen). Returns true to reconnect, false to shut down.
func (c *Controller) runFanControlDutyCycle() bool {
	client := c.client

	disconnected := client.Disconnected()

	// The Display Image transfer is many BLE frames and takes hundreds of ms, so
	// it runs on a dedicated uploader goroutine scoped to this connection: the
	// control loop only enqueues the desired icon (instant) and a newer icon
	// preempts an in-flight upload. This keeps mode changes and duty writes
	// responsive while an icon is still streaming.
	uploadCtx, stopUploader := context.WithCancel(c.ctx)
	state := fanControlState{display: make(chan fanDisplayJob, 1)}

	uploaderDone := make(chan struct{})

	go func() {
		defer close(uploaderDone)

		c.runFanDisplayUploader(uploadCtx, client, state.display)
	}()

	defer func() {
		stopUploader()
		<-uploaderDone
	}()

	// Show the current mode on the device screen as soon as the link is up
	// (covers both initial connect and every reconnect). A fresh link never holds
	// control, so the icon starts in its manual-control (grey) colour.
	c.requestFanModeDisplay(&state, false)

	// Periodically re-evaluate config so enable/disable and device-selection
	// changes take effect live, without waiting for the BLE link to drop.
	configCheck := time.NewTicker(fanConfigCheckInterval)
	defer configCheck.Stop()

	for {
		select {
		case <-c.ctx.Done():
			c.releaseFanControl(client, &state)

			return false

		case <-disconnected:
			c.log.Warn().
				Str("component", "fan").
				Msg("BLE device disconnected")

			return true

		case <-configCheck.C:
			if c.fanConfigChanged(client) {
				c.log.Info().Str("component", "fan").Msg("Config changed, reconfiguring")
				c.stopFanForReconfigure(client, &state)

				return true
			}

		case event := <-c.eventChan:
			c.handleFanEvent(event, &state)

		case cmd := <-c.controlChan:
			if !c.applyFanCommand(client, cmd, &state) {
				// Reconnect on a genuine command error; return false (shut down) if
				// the context was cancelled out from under the command.
				return c.ctx.Err() == nil
			}
		}
	}
}

// applyFanCommand reconciles the device with one fanCommand. When the app should
// drive, it takes control on the first drive and writes the duty; when it should
// not drive, it releases any held control. It returns false on a BLE command
// error so the caller can reconnect.
func (c *Controller) applyFanCommand(client fanClient, cmd fanCommand, state *fanControlState) bool {
	if !cmd.drive {
		// The app is idle this tick: hand control back to the device, which
		// restores the user's encoder setpoint. If control was actually released,
		// recolour the mode icon back to its manual-control (grey) colour.
		if c.releaseFanControl(client, state) {
			c.requestFanModeDisplay(state, false)
		}

		return true
	}

	cmdTimeout := time.Duration(c.config.GetFanCommandTimeoutMs()) * time.Millisecond

	cmdCtx, cancel := context.WithTimeout(c.ctx, cmdTimeout)
	defer cancel()

	if !state.controlHeld {
		err := client.TakeControl(cmdCtx)
		if err != nil {
			return c.logFanCommandErr(err)
		}

		state.controlHeld = true

		// The app now drives the fan; recolour the mode icon to its host-control
		// colour (indigo for "auto", deep purple for "all").
		c.requestFanModeDisplay(state, true)
	}

	_, err := client.SetFanDuty(cmdCtx, cmd.duty)
	if err != nil {
		return c.logFanCommandErr(err)
	}

	return true
}

// logFanCommandErr logs a BLE command failure (unless the context was cancelled
// during shutdown) and always returns false so callers can signal a reconnect.
func (c *Controller) logFanCommandErr(err error) bool {
	if c.ctx.Err() == nil {
		c.log.Warn().
			Err(err).
			Str("component", "fan").
			Msg("Command failed")
	}

	return false
}

// releaseFanControl hands control back to the device if the controller currently holds
// it, so the device restores the user's manual setpoint. It returns true if it
// actually released control (so the caller can refresh the screen) and false
// when the controller was not holding control.
func (c *Controller) releaseFanControl(client fanClient, state *fanControlState) bool {
	if !state.controlHeld {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), fanShutdownTimeout)
	defer cancel()

	err := client.ReleaseControl(ctx)
	if err != nil {
		c.log.Debug().
			Err(err).
			Str("component", "fan").
			Msg("release control failed")
	}

	state.controlHeld = false

	return true
}

// handleFanEvent reacts to a Fan Event notification from the device.
func (c *Controller) handleFanEvent(event fancontroller.Event, state *fanControlState) {
	switch event {
	case fancontroller.EventButton:
		// The device's front button cycles the fan mode; reflect the new mode back
		// to its screen, keeping the current control colour.
		mode := c.config.CycleFanMode(true)
		c.log.Info().Str("component", "fan").Str("mode", mode).Msg("Mode cycled by device button")
		c.requestFanModeDisplay(state, state.controlHeld)

	case fancontroller.EventImageOK:
		// A mode icon transfer completed and rendered; nothing to do.

	case fancontroller.EventImageAbort:
		// The device rejected the mode icon transfer. The icon is cosmetic and the
		// next mode change re-sends it, so just log it.
		c.log.Warn().Str("component", "fan").Msg("Display image transfer aborted by device")
	}
}

// requestFanModeDisplay queues the current mode's icon for asynchronous upload in
// the colour implied by hostControl (grey when the device holds control, the
// mode's active colour when the app drives the fan). It never blocks the control
// loop: the latest icon wins, replacing any still-queued job, and an in-flight
// upload is preempted by the uploader when it sees the new job.
func (c *Controller) requestFanModeDisplay(state *fanControlState, hostControl bool) {
	mode := c.config.GetFanMode()
	job := fanDisplayJob{
		name:       fanModeIcon(mode),
		foreground: fanModeIconColor(mode, hostControl),
	}

	select {
	case state.display <- job:
	default:
		// Buffer full: drop the stale queued job and enqueue the newest one.
		select {
		case <-state.display:
		default:
		}

		select {
		case state.display <- job:
		default:
		}
	}
}

// runFanDisplayUploader serialises Display Image uploads for one connection,
// uploading queued jobs one at a time until ctx is cancelled.
func (c *Controller) runFanDisplayUploader(ctx context.Context, client fanClient, jobs <-chan fanDisplayJob) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-jobs:
			c.uploadFanDisplay(ctx, client, job, jobs)
		}
	}
}

// uploadFanDisplay uploads job, restarting with a newer job if one is queued
// mid-transfer. Preemption cancels the in-flight upload before COMMIT, so the
// device discards the partial and only ever renders a complete icon.
func (c *Controller) uploadFanDisplay(ctx context.Context, client fanClient, job fanDisplayJob, jobs <-chan fanDisplayJob) {
	for {
		upCtx, cancel := context.WithTimeout(ctx, fanDisplayUploadTimeout)
		done := make(chan struct{})

		go func() {
			defer close(done)

			c.sendFanDisplayJob(upCtx, client, job)
		}()

		select {
		case <-ctx.Done():
			cancel()
			<-done

			return

		case next := <-jobs:
			// A newer icon supersedes this one: halt the in-flight upload and retry.
			cancel()
			<-done

			job = next

		case <-done:
			cancel()

			return
		}
	}
}

// sendFanDisplayJob renders job's icon and streams it to the device screen,
// honouring ctx so a preempted or shutting-down upload stops quietly.
func (c *Controller) sendFanDisplayJob(ctx context.Context, client fanClient, job fanDisplayJob) {
	icon, err := fanModeIconImage(job.name, job.foreground)
	if err != nil {
		c.log.Warn().
			Err(err).
			Str("component", "fan").
			Msg("render fan mode icon failed")

		return
	}

	started := time.Now()

	err = client.SetDisplayImage(ctx, icon.pixels, icon.width, icon.height)
	if err != nil {
		if ctx.Err() != nil {
			return // preempted or shutting down: not a real failure
		}

		c.log.Warn().
			Err(err).
			Str("component", "fan").
			Msg("set display image failed")

		return
	}

	c.log.Info().
		Str("component", "fan").
		Str("icon", job.name).
		Int("bytes", len(icon.pixels)).
		Dur("elapsed", time.Since(started)).
		Msg("Display image uploaded")
}

// fanModeIconSize is the square canvas every mode icon is rendered to, for a
// uniform on-screen size. Each icon is aspect-fit and centred within it; the
// device then centres the square in its 150×50 display box.
const fanModeIconSize = 50

type fanModeIconData struct {
	pixels        []byte
	width, height int
}

// fanModeIconImage rasterises the named SVG to fit the display box and packs it
// to little-endian RGB565 (fg on black). Rendering is cheap and only happens on a
// mode or control-state change, so the result is not cached.
func fanModeIconImage(name string, foreground color.Color) (fanModeIconData, error) {
	mask, err := icons.RenderFit(name, fanModeIconSize, fanModeIconSize)
	if err != nil {
		return fanModeIconData{}, err
	}

	return fanModeIconData{
		pixels: fancontroller.AlphaToRGB565LE(mask, foreground, color.Black),
		width:  mask.Bounds().Dx(),
		height: mask.Bounds().Dy(),
	}, nil
}

// fanModeIconColor picks the mode icon's foreground colour. When the device holds
// control (hostControl false) every mode is grey. When the app drives the fan,
// "auto" turns indigo and "all" turns deep purple to signal active host control;
// "manual" never drives, so it stays grey.
func fanModeIconColor(mode string, hostControl bool) color.Color {
	grey := gui.MaterialGrey()

	if !hostControl {
		return grey
	}

	switch mode {
	case "auto":
		return gui.MaterialIndigo()
	case "all":
		return gui.MaterialDeepPurple()
	default:
		return grey
	}
}

// fanModeIcon maps a fan mode to the SVG icon name shown on the device screen:
// "manual" → a fan symbol, "auto" → the wind glyph with auto badge, "all" →
// the wind glyph with an infinity badge.
func fanModeIcon(mode string) string {
	switch mode {
	case "auto":
		return "wind-auto"
	case "all":
		return "wind-all"
	default:
		return "fan"
	}
}

// fanConfigChanged reports whether the live config no longer matches the
// connected client: experimental features were turned off, the fan was
// disabled, or a different device was selected. In any of these cases the
// current connection must be torn down so the outer loop reconciles to the new
// desired state (idle when disabled, reconnect when the address changed).
func (c *Controller) fanConfigChanged(client fanClient) bool {
	if !c.config.GetExperimentalFeaturesEnabled() || !c.config.FanEnabled() {
		return true
	}

	return c.config.GetFanDeviceAddress() != client.DeviceAddress()
}

// stopFanForReconfigure hands control back to the device before the connection is
// closed for a live config change, so a disabled or reselected fan reverts to the
// user's manual setpoint immediately rather than coasting at the app's last duty.
func (c *Controller) stopFanForReconfigure(client fanClient, state *fanControlState) {
	c.releaseFanControl(client, state)
}

// HandleControlTick is called by mainLoop at the wind sim frame rate. When
// wind simulation is active it sends the speed-based duty for the app to drive;
// otherwise it signals "not driving" so the duty-cycle loop hands control back to
// the device, whose rotary encoder then controls the fan locally.
func (c *Controller) HandleControlTick() {
	if c.controlChan == nil {
		return
	}

	// The fan/wind simulator is gated behind experimental features.
	if !c.config.GetExperimentalFeaturesEnabled() {
		return
	}

	cmd := fanCommand{drive: false}

	if c.shouldUpdateFanSpeed() {
		cmd = fanCommand{drive: true, duty: c.calculateFanDutyCycle()}
	}

	select {
	case c.controlChan <- cmd:
	default:
	}
}

// calculateFanDutyCycle returns the fan duty (0-100) based on current vehicle speed.
// Duty scales linearly from 0% at 0 km/h to 100% at the configured max speed.
// Conditions mirror haptic control: wind is stopped when paused, during replays
// (unless enableReplay is set), or in the post-race menu.
func (c *Controller) calculateFanDutyCycle() int {
	maxKPH := float32(c.config.GetFanMaxSpeedKPH())
	if maxKPH <= 0 {
		maxKPH = fanMaxSpeedKPH
	}

	speedKPH := c.telemetry().GroundSpeedKPH()
	duty := int(speedKPH / maxKPH * 100)

	switch {
	case duty < 0:
		return 0
	case duty > 100:
		return 100
	default:
		return duty
	}
}

func (c *Controller) shouldUpdateFanSpeed() bool {
	if !c.telemetry().TelemetryStarted() {
		return false
	}

	if !c.telemetry().IsOnCircuit() {
		return false
	}

	if !c.telemetryActive() {
		return false
	}

	if c.inPostRaceMenu() {
		return false
	}

	return c.shouldSimulateWindForCurrentVehicle()
}

func (c *Controller) shouldSimulateWindForCurrentVehicle() bool {
	switch c.config.GetFanMode() {
	case "all":
		return true
	case "auto":
		return c.telemetry().VehicleHasOpenCockpit()
	default:
		return false
	}
}
