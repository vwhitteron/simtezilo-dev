package app

// The engine haptic generator and the vehicle's engine-characteristics derivation both
// live in app/haptics (engine.go, engine_profile.go), so the CGO-free capture can drive
// them. What remains here is the delegation from App, which owns the session gates and
// the sequence number.

// generateEngineHaptic drives the engine haptic for the current tick.
//
// The calibration and haptics-enabled gates live here rather than in the generator,
// because they are app session state. This mirrors how generateForceHaptics gates the
// chassis layer.
func (a *App) generateEngineHaptic() {
	if a.calibrator.IsEnabled() || !a.state.hapticsEnabled {
		return
	}

	a.engineGen.Generate(a.state.current.sequenceNumber)
}
