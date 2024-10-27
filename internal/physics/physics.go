package physics

import (
	telemetry_client "github.com/vwhitteron/gt-telemetry"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/symmetryaxis"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/vector"
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

	TransmissionGear int

	AudioOutValue float64
}

type PhysicsTracker struct {
	Last    Physics
	Current Physics
}

func NewPhysicsTracker() PhysicsTracker {
	return PhysicsTracker{
		Last:    newPhysics(),
		Current: newPhysics(),
	}
}

func newPhysics() Physics {
	return Physics{
		AttitudeAcceleration: 0,
		AttitudeJerk:         0,
		AttitudeSnap:         0,
		AttitudeDelta:        telemetry_client.SymmetryAxes{},
		AttitudeVector:       telemetry_client.SymmetryAxes{},
		Acceleration:         0,
		Jerk:                 0,
		Snap:                 0,
		Crackle:              0,
		VelocityDelta:        telemetry_client.Vector{},
		VelocityVector:       telemetry_client.Vector{},
		TransmissionGear:     -100,
		AudioOutValue:        0,
	}
}

// FIXME: ideally this should not be given the gt client
func (t *PhysicsTracker) Update(windowMilliseconds float64, gtclient *telemetry_client.GTClient) {
	t.Current.VelocityVector = gtclient.Telemetry.VelocityVector()
	t.Current.AttitudeVector = gtclient.Telemetry.RotationVector()

	// chassis attitude
	t.Current.AttitudeDelta = symmetryaxis.Delta(t.Current.AttitudeVector, t.Last.AttitudeVector)

	// ignore yaw jerk/snap as it causes vibration during heavy rotation (high G-force corners, spin out, etc)
	biasedAttitudeDelta := symmetryaxis.Scale(t.Current.AttitudeDelta, 1.0, 0.25, 1.0)

	t.Current.AttitudeAcceleration = symmetryaxis.Magnitude(biasedAttitudeDelta) / windowMilliseconds

	t.Last.AttitudeJerk = t.Current.AttitudeJerk
	t.Current.AttitudeJerk = (t.Current.AttitudeAcceleration - t.Last.AttitudeAcceleration) / windowMilliseconds
	if t.Current.AttitudeJerk > 10 {
		t.Current.AttitudeJerk = 10
	} else if t.Current.AttitudeJerk < -10 {
		t.Current.AttitudeJerk = -10
	}

	t.Last.AttitudeSnap = t.Current.AttitudeSnap
	t.Current.AttitudeSnap = (t.Current.AttitudeJerk - t.Last.AttitudeJerk) / windowMilliseconds

	// chassis position
	t.Current.VelocityDelta = vector.Delta(t.Current.VelocityVector, t.Last.VelocityVector)
	t.Current.Acceleration = vector.Magnitude(t.Current.VelocityDelta) / windowMilliseconds

	t.Last.Jerk = t.Current.Jerk
	t.Current.Jerk = (t.Current.Acceleration - t.Last.Acceleration) / windowMilliseconds

	t.Last.Snap = t.Current.Snap
	t.Current.Snap = (t.Current.Jerk - t.Last.Jerk) / windowMilliseconds

	t.Last.Crackle = t.Current.Crackle
	t.Current.Crackle = (t.Current.Snap - t.Last.Snap) / windowMilliseconds

	t.Last.TransmissionGear = t.Current.TransmissionGear
	t.Current.TransmissionGear = gtclient.Telemetry.CurrentGear()

	t.Last.SequenceID = t.Current.SequenceID
}
