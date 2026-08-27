package app

// The gear-shift haptic itself lives in app/haptics (transmission.go), so offline
// tooling can drive the real generator without linking an audio backend. What remains
// here is the delegation from App, which owns the vehicle and the sequence number the
// generator needs.

import (
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
)

// telemetrySource reads the telemetry transformer at call time rather than capturing
// it. The GT client is built after the generators and is rebuilt whenever the
// telemetry settings change, so a captured source would be nil at first and stale
// afterwards.
func (a *App) telemetrySource() haptics.TelemetrySource { //nolint:ireturn // the generators read telemetry through this interface by design, so a fake can drive them
	return a.gtClient.Telemetry
}

// setTransmissionGain re-seeds the transmission generator for a newly selected
// vehicle, which sets the gain floor from the vehicle type and seeds the learned
// gear-shift harshness.
//
// The vehicle type is passed separately because updateVehicle calls this while
// building a.vehicle, and the two must agree.
func (a *App) setTransmissionGain(vehicleType vehicle.TypeName) {
	characteristics := a.vehicle
	characteristics.VehicleType = vehicleType

	a.transmissionGen.SetVehicle(characteristics)
}

// playGearShiftHaptic plays the gear-change haptic for the current telemetry frame.
func (a *App) playGearShiftHaptic() {
	a.transmissionGen.PlayGearShift(a.state.current.sequenceNumber)
}

// advanceGearShiftDriveline samples this frame's driveline state. It must run after
// kinematics.Update sets the current gear and before the gear-change handler.
func (a *App) advanceGearShiftDriveline() {
	a.transmissionGen.AdvanceDriveline()
}

// tickGearShiftMeasurement advances an in-flight gear-shift measurement by one frame.
func (a *App) tickGearShiftMeasurement() {
	a.transmissionGen.TickMeasurement()
}
