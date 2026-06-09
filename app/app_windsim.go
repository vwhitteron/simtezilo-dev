package app

import (
	"context"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/hardware/fancontroller"
)

const (
	fanConnectTimeout  = 30 * time.Second
	fanReconnectDelay  = 10 * time.Second
	fanModeCheckDelay  = 5 * time.Second
	fanShutdownTimeout = 2 * time.Second
	fanMaxSpeedKPH     = 250.0
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

	a.fanControlChan = make(chan int, 1)

	a.log.Info().
		Str("component", "fan").
		Str("mode", a.config.GetFanMode()).
		Str("deviceName", a.config.GetFanDeviceName()).
		Msg("initialized")
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

		if !a.config.GetExperimentalFeaturesEnabled() || !a.config.FanEnabled() ||
			(a.config.GetFanMode() == "manual" && a.getManualFanDuty() == 0) {
			select {
			case <-time.After(fanModeCheckDelay):
			case <-a.ctx.Done():
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

			select {
			case <-time.After(fanReconnectDelay):
			case <-a.ctx.Done():
				return
			}

			continue
		}

		a.fanController = client

		a.log.Info().Str("component", "fan").Str("deviceName", client.DeviceName()).Msg("Connected")

		reconnect := a.runFanControlDutyCycle()

		a.fanController = nil

		_ = client.Close()

		if !reconnect {
			return
		}

		a.log.Info().Str("component", "fan").Msg("Connection lost, reconnecting")

		select {
		case <-time.After(fanReconnectDelay):
		case <-a.ctx.Done():
			return
		}
	}
}

// createFanControllerClient builds a new fancontroller.Client from the current config.
func (a *App) createFanControllerClient() *fancontroller.Client {
	cmdTimeout := time.Duration(a.config.GetFanCommandTimeoutMs()) * time.Millisecond

	return fancontroller.New(fancontroller.Options{
		DeviceName:     a.config.GetFanDeviceName(),
		CommandTimeout: cmdTimeout,
	})
}

// runFanControlDutyCycle reads duty values sent by handleFanControlTick via the duty channel
// and forwards them as BLE commands. The correct duty (wind sim or manual fallback)
// is already calculated by handleFanControlTick. Returns true to reconnect, false to shut down.
func (a *App) runFanControlDutyCycle() bool {
	client := a.fanController

	disconnected := client.Disconnected()

	for {
		select {
		case <-a.ctx.Done():
			shutCtx, cancel := context.WithTimeout(context.Background(), fanShutdownTimeout)
			_, _ = client.SetFanDuty(shutCtx, 0)

			cancel()

			return false

		case <-disconnected:
			a.log.Warn().
				Str("component", "fan").
				Msg("BLE device disconnected")

			return true

		case duty := <-a.fanControlChan:
			cmdTimeout := time.Duration(a.config.GetFanCommandTimeoutMs()) * time.Millisecond
			cmdCtx, cancel := context.WithTimeout(a.ctx, cmdTimeout)
			_, err := client.SetFanDuty(cmdCtx, duty)

			cancel()

			if err != nil {
				if a.ctx.Err() != nil {
					return false
				}

				a.log.Warn().
					Err(err).
					Str("component", "fan").
					Msg("Command failed")

				return true
			}
		}
	}
}

// handleFanControlTick is called by mainLoop at the wind sim frame rate.
// It calculates the desired fan duty and sends it to the BLE goroutine.
// When wind simulation is active (on circuit), the speed-based duty is used
// directly. Otherwise, the manual fan duty applies as a constant baseline.
func (a *App) handleFanControlTick() {
	if a.fanControlChan == nil {
		return
	}

	// The fan/wind simulator is gated behind experimental features.
	if !a.config.GetExperimentalFeaturesEnabled() {
		return
	}

	duty := a.getManualFanDuty()

	if a.shouldUpdateFanSpeed() {
		duty = a.calculateFanDutyCycle()
	}

	select {
	case a.fanControlChan <- duty:
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

// getManualFanDuty returns the manual fan duty cycle (0-100) from the 0-10 step setting.
func (a *App) getManualFanDuty() int {
	return int(a.manualFanDuty.Load()) * 10
}

// setManualFanDuty sets the manual fan speed step (0-10) and returns the clamped value.
func (a *App) setManualFanDuty(speed int) int {
	if speed < 0 {
		speed = 0
	}

	if speed > 10 {
		speed = 10
	}

	a.manualFanDuty.Store(int32(speed)) //nolint:gosec // speed is clamped to 0-10

	return speed
}
