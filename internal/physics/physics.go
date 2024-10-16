package physics

import (
	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

type Physics struct {
	SequenceID uint32

	AttitudeAcceleration float64
	AttitudeJerk         float64
	AttitudeSnap         float64
	AttitudeDelta        telemetry_client.SymmetryAxes
	AttitudeVector       telemetry_client.SymmetryAxes

	Acceleration   float64
	Jerk           float64
	Snap           float64
	Crackle        float64
	VelocityDelta  telemetry_client.Vector
	VelocityVector telemetry_client.Vector

	AudioOutValue float64
}

type PhysicsTracker struct {
	Last    Physics
	Current Physics
}
