package app

import (
	"context"
	"image/color"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/fancontroller"
	"github.com/vwhitteron/simtezilo-dev/app/ui/gui"
	"github.com/vwhitteron/simtezilo-dev/app/ui/icons"
)

// fanClient is the subset of *fancontroller.Client used by the app, extracted
// so the control loop can be tested with a fake.
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

// initializeFanController sets up the fan controller channel and logs the configured mode.
// The BLE goroutine is always started and waits for the mode to become active.
func (a *App) initializeFanController() {
	if !a.config.IsFanModeValid() {
		a.log.Warn().
			Str("component", "fan").
			Str("configuredMode", a.config.GetFanConfiguredMode()).
			Str("fallbackMode", "manual").
			Msg("Invalid fan mode configured, falling back to manual")
	}

	a.fanControlChan = make(chan fanCommand, 1)
	a.fanEventChan = make(chan fancontroller.Event, 8)

	a.log.Info().
		Str("component", "fan").
		Str("mode", a.config.GetFanMode()).
		Str("address", a.config.GetFanDeviceAddress()).
		Msg("Initialized")
}

// startFanControllerTask starts the wind simulator background goroutine.
func (a *App) startFanControllerTask() {
	a.wg.Add(1)

	go func() {
		defer func() {
			a.log.Debug().Msg("Fan control goroutine exiting")
			a.wg.Done()
		}()

		a.runFanControlTask()
	}()
}

// closeFanController cleanly closes the wind simulator client connection.
func (a *App) closeFanController() {
	if a.fanController == nil {
		return
	}

	a.log.Info().Msg("Closing fan controller client")

	err := a.fanController.Close()
	if err != nil {
		a.log.Error().
			Err(err).
			Str("component", "fan").
			Str("result", "failure").
			Msg("Close")
	}

	a.log.Info().Msg("Fan controller close phase complete")
}

// runFanControlTask connects to the fan device and maintains the output duty cycle, reconnecting on errors.
// When mode is "manual" it waits, polling periodically for the mode to change.
func (a *App) runFanControlTask() {
	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}

		if a.fanShouldIdle() {
			if a.fanWait(fanModeCheckDelay) {
				return
			}

			continue
		}

		a.log.Info().Str("component", "fan").Msg("Scanning for device")

		client := a.createFanControllerClient()

		connectCtx, connectCancel := context.WithTimeout(a.ctx, fanConnectTimeout)
		err := client.Connect(connectCtx)

		connectCancel()

		if err != nil {
			a.log.Warn().
				Err(err).
				Str("component", "fan").
				Msg("Connect failed, retrying")

			if a.fanWait(fanReconnectDelay) {
				return
			}

			continue
		}

		a.fanController = client

		a.log.Info().Str("component", "fan").Str("address", client.DeviceAddress()).Msg("Connected")

		reconnect := a.runFanControlDutyCycle()

		a.fanController = nil

		_ = client.Close()

		if !reconnect {
			return
		}

		a.log.Info().Str("component", "fan").Msg("Connection lost, reconnecting")

		if a.fanWait(fanReconnectDelay) {
			return
		}
	}
}

// fanWait sleeps for delay or until the app context is cancelled. It returns true
// if the context was cancelled (the caller should stop), false if the delay
// elapsed normally.
func (a *App) fanWait(delay time.Duration) bool {
	select {
	case <-time.After(delay):
		return false
	case <-a.ctx.Done():
		return true
	}
}

// fanShouldIdle reports whether the fan control task should stay idle rather than
// hold a connection. The link is kept up whenever the fan is enabled and an
// address is set, regardless of mode: the device's button (mode cycling) and
// screen (mode display) need a live link even in manual mode, when the app never
// drives the fan itself.
func (a *App) fanShouldIdle() bool {
	return !a.config.GetExperimentalFeaturesEnabled() ||
		!a.config.FanEnabled() ||
		a.config.GetFanDeviceAddress() == ""
}

// createFanControllerClient builds a new fancontroller.Client from the current config.
func (a *App) createFanControllerClient() *fancontroller.Client {
	cmdTimeout := time.Duration(a.config.GetFanCommandTimeoutMs()) * time.Millisecond

	return fancontroller.New(fancontroller.Options{
		Address:        a.config.GetFanDeviceAddress(),
		CommandTimeout: cmdTimeout,
		// OnEvent runs on the BLE notification goroutine and must not block, so it
		// hands the event to the duty-cycle loop via a buffered channel. A full
		// channel drops the event rather than stalling the BLE stack.
		OnEvent: func(e fancontroller.Event) {
			select {
			case a.fanEventChan <- e:
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
func (a *App) runFanControlDutyCycle() bool {
	client := a.fanController

	disconnected := client.Disconnected()

	// The Display Image transfer is many BLE frames and takes hundreds of ms, so
	// it runs on a dedicated uploader goroutine scoped to this connection: the
	// control loop only enqueues the desired icon (instant) and a newer icon
	// preempts an in-flight upload. This keeps mode changes and duty writes
	// responsive while an icon is still streaming.
	uploadCtx, stopUploader := context.WithCancel(a.ctx)
	state := fanControlState{display: make(chan fanDisplayJob, 1)}

	uploaderDone := make(chan struct{})

	go func() {
		defer close(uploaderDone)

		a.runFanDisplayUploader(uploadCtx, client, state.display)
	}()

	defer func() {
		stopUploader()
		<-uploaderDone
	}()

	// Show the current mode on the device screen as soon as the link is up
	// (covers both initial connect and every reconnect). A fresh link never holds
	// control, so the icon starts in its manual-control (grey) colour.
	a.requestFanModeDisplay(&state, false)

	// Periodically re-evaluate config so enable/disable and device-selection
	// changes take effect live, without waiting for the BLE link to drop.
	configCheck := time.NewTicker(fanConfigCheckInterval)
	defer configCheck.Stop()

	for {
		select {
		case <-a.ctx.Done():
			a.releaseFanControl(client, &state)

			return false

		case <-disconnected:
			a.log.Warn().
				Str("component", "fan").
				Msg("BLE device disconnected")

			return true

		case <-configCheck.C:
			if a.fanConfigChanged(client) {
				a.log.Info().Str("component", "fan").Msg("Config changed, reconfiguring")
				a.stopFanForReconfigure(client, &state)

				return true
			}

		case event := <-a.fanEventChan:
			a.handleFanEvent(event, &state)

		case cmd := <-a.fanControlChan:
			if !a.applyFanCommand(client, cmd, &state) {
				// Reconnect on a genuine command error; return false (shut down) if
				// the context was cancelled out from under the command.
				return a.ctx.Err() == nil
			}
		}
	}
}

// applyFanCommand reconciles the device with one fanCommand. When the app should
// drive, it takes control on the first drive and writes the duty; when it should
// not drive, it releases any held control. It returns false on a BLE command
// error so the caller can reconnect.
func (a *App) applyFanCommand(client fanClient, cmd fanCommand, state *fanControlState) bool {
	if !cmd.drive {
		// The app is idle this tick: hand control back to the device, which
		// restores the user's encoder setpoint. If control was actually released,
		// recolour the mode icon back to its manual-control (grey) colour.
		if a.releaseFanControl(client, state) {
			a.requestFanModeDisplay(state, false)
		}

		return true
	}

	cmdTimeout := time.Duration(a.config.GetFanCommandTimeoutMs()) * time.Millisecond

	cmdCtx, cancel := context.WithTimeout(a.ctx, cmdTimeout)
	defer cancel()

	if !state.controlHeld {
		err := client.TakeControl(cmdCtx)
		if err != nil {
			return a.logFanCommandErr(err)
		}

		state.controlHeld = true

		// The app now drives the fan; recolour the mode icon to its host-control
		// colour (indigo for "open", deep purple for "all").
		a.requestFanModeDisplay(state, true)
	}

	_, err := client.SetFanDuty(cmdCtx, cmd.duty)
	if err != nil {
		return a.logFanCommandErr(err)
	}

	return true
}

// logFanCommandErr logs a BLE command failure (unless the context was cancelled
// during shutdown) and always returns false so callers can signal a reconnect.
func (a *App) logFanCommandErr(err error) bool {
	if a.ctx.Err() == nil {
		a.log.Warn().
			Err(err).
			Str("component", "fan").
			Msg("Command failed")
	}

	return false
}

// releaseFanControl hands control back to the device if the app currently holds
// it, so the device restores the user's manual setpoint. It returns true if it
// actually released control (so the caller can refresh the screen) and false
// when the app was not holding control.
func (a *App) releaseFanControl(client fanClient, state *fanControlState) bool {
	if !state.controlHeld {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), fanShutdownTimeout)
	defer cancel()

	err := client.ReleaseControl(ctx)
	if err != nil {
		a.log.Debug().
			Err(err).
			Str("component", "fan").
			Msg("release control failed")
	}

	state.controlHeld = false

	return true
}

// handleFanEvent reacts to a Fan Event notification from the device.
func (a *App) handleFanEvent(event fancontroller.Event, state *fanControlState) {
	switch event {
	case fancontroller.EventButton:
		// The device's front button cycles the fan mode; reflect the new mode back
		// to its screen, keeping the current control colour.
		mode := a.config.CycleFanMode(true)
		a.log.Info().Str("component", "fan").Str("mode", mode).Msg("Mode cycled by device button")
		a.requestFanModeDisplay(state, state.controlHeld)

	case fancontroller.EventImageOK:
		// A mode icon transfer completed and rendered; nothing to do.

	case fancontroller.EventImageAbort:
		// The device rejected the mode icon transfer. The icon is cosmetic and the
		// next mode change re-sends it, so just log it.
		a.log.Warn().Str("component", "fan").Msg("Display image transfer aborted by device")
	}
}

// requestFanModeDisplay queues the current mode's icon for asynchronous upload in
// the colour implied by hostControl (grey when the device holds control, the
// mode's active colour when the app drives the fan). It never blocks the control
// loop: the latest icon wins, replacing any still-queued job, and an in-flight
// upload is preempted by the uploader when it sees the new job.
func (a *App) requestFanModeDisplay(state *fanControlState, hostControl bool) {
	mode := a.config.GetFanMode()
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
func (a *App) runFanDisplayUploader(ctx context.Context, client fanClient, jobs <-chan fanDisplayJob) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-jobs:
			a.uploadFanDisplay(ctx, client, job, jobs)
		}
	}
}

// uploadFanDisplay uploads job, restarting with a newer job if one is queued
// mid-transfer. Preemption cancels the in-flight upload before COMMIT, so the
// device discards the partial and only ever renders a complete icon.
func (a *App) uploadFanDisplay(ctx context.Context, client fanClient, job fanDisplayJob, jobs <-chan fanDisplayJob) {
	for {
		upCtx, cancel := context.WithTimeout(ctx, fanDisplayUploadTimeout)
		done := make(chan struct{})

		go func() {
			defer close(done)

			a.sendFanDisplayJob(upCtx, client, job)
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
func (a *App) sendFanDisplayJob(ctx context.Context, client fanClient, job fanDisplayJob) {
	icon, err := fanModeIconImage(job.name, job.foreground)
	if err != nil {
		a.log.Warn().
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

		a.log.Warn().
			Err(err).
			Str("component", "fan").
			Msg("set display image failed")

		return
	}

	a.log.Info().
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
// "open" turns indigo and "all" turns deep purple to signal active host control;
// "manual" never drives, so it stays grey.
func fanModeIconColor(mode string, hostControl bool) color.Color {
	grey := gui.MaterialGrey()

	if !hostControl {
		return grey
	}

	switch mode {
	case "open":
		return gui.MaterialIndigo()
	case "all":
		return gui.MaterialDeepPurple()
	default:
		return grey
	}
}

// fanConfigChanged reports whether the live config no longer matches the
// connected client: experimental features were turned off, the fan was
// disabled, or a different device was selected. In any of these cases the
// current connection must be torn down so the outer loop reconciles to the new
// desired state (idle when disabled, reconnect when the address changed).
func (a *App) fanConfigChanged(client fanClient) bool {
	if !a.config.GetExperimentalFeaturesEnabled() || !a.config.FanEnabled() {
		return true
	}

	return a.config.GetFanDeviceAddress() != client.DeviceAddress()
}

// stopFanForReconfigure hands control back to the device before the connection is
// closed for a live config change, so a disabled or reselected fan reverts to the
// user's manual setpoint immediately rather than coasting at the app's last duty.
func (a *App) stopFanForReconfigure(client fanClient, state *fanControlState) {
	a.releaseFanControl(client, state)
}

// handleFanControlTick is called by mainLoop at the wind sim frame rate. When
// wind simulation is active it sends the speed-based duty for the app to drive;
// otherwise it signals "not driving" so the duty-cycle loop hands control back to
// the device, whose rotary encoder then controls the fan locally.
func (a *App) handleFanControlTick() {
	if a.fanControlChan == nil {
		return
	}

	// The fan/wind simulator is gated behind experimental features.
	if !a.config.GetExperimentalFeaturesEnabled() {
		return
	}

	cmd := fanCommand{drive: false}

	if a.shouldUpdateFanSpeed() {
		cmd = fanCommand{drive: true, duty: a.calculateFanDutyCycle()}
	}

	select {
	case a.fanControlChan <- cmd:
	default:
	}
}

// calculateFanDutyCycle returns the fan duty (0-100) based on current vehicle speed.
// Duty scales linearly from 0% at 0 km/h to 100% at the configured max speed.
// Conditions mirror haptic control: wind is stopped when paused, during replays
// (unless enableReplay is set), or in the post-race menu.
func (a *App) calculateFanDutyCycle() int {
	maxKPH := float32(a.config.GetFanMaxSpeedKPH())
	if maxKPH <= 0 {
		maxKPH = fanMaxSpeedKPH
	}

	speedKPH := a.gtClient.Telemetry.GroundSpeedKPH()
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

func (a *App) shouldUpdateFanSpeed() bool {
	if !a.gtClient.Telemetry.TelemetryStarted() {
		return false
	}

	if !a.gtClient.Telemetry.IsOnCircuit() {
		return false
	}

	if !a.telemetryIsActive() {
		return false
	}

	if a.state.isInPostRaceMenu {
		return false
	}

	return a.shouldSimulateWindForCurrentVehicle()
}

func (a *App) shouldSimulateWindForCurrentVehicle() bool {
	switch a.config.GetFanMode() {
	case "all":
		return true
	case "open":
		return a.gtClient.Telemetry.VehicleHasOpenCockpit()
	default:
		return false
	}
}
