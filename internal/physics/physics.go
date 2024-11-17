package physics

import (
	telemetry_client "github.com/vwhitteron/gt-telemetry"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/symmetryaxis"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/vector"
)

type VelocityDerivatives struct {
	Delta        telemetry_client.Vector
	Vector       telemetry_client.Vector
	Acceleration float64
	Jerk         float64
	Snap         float64
	Crackle      float64
}

type AttitudeDerivatives struct {
	Delta        telemetry_client.SymmetryAxes
	Vector       telemetry_client.SymmetryAxes
	Acceleration float64
	Jerk         float64
	Snap         float64
	Crackle      float64
}

type Physics struct {
	SequenceID uint32

	Attitude AttitudeDerivatives
	Velocity VelocityDerivatives

	TransmissionGear int

	SynthOutputAmplitude float64
	SynthOutputFrequency int
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
		Attitude: AttitudeDerivatives{
			Delta:        telemetry_client.SymmetryAxes{},
			Vector:       telemetry_client.SymmetryAxes{},
			Acceleration: 0,
			Jerk:         0,
			Snap:         0,
			Crackle:      0,
		},
		Velocity: VelocityDerivatives{
			Delta:        telemetry_client.Vector{},
			Vector:       telemetry_client.Vector{},
			Acceleration: 0,
			Jerk:         0,
			Snap:         0,
			Crackle:      0,
		},
		TransmissionGear:     -100,
		SynthOutputAmplitude: 0,
		SynthOutputFrequency: 0,
	}
}

// FIXME: ideally this should not be given the gt client
func (t *PhysicsTracker) Update(windowMilliseconds float64, gtclient *telemetry_client.GTClient) {
	t.Current.Velocity.Vector = gtclient.Telemetry.VelocityVector()
	t.Current.Attitude.Vector = gtclient.Telemetry.RotationVector()

	// chassis attitude
	t.Current.Attitude.Delta = symmetryaxis.Delta(t.Current.Attitude.Vector, t.Last.Attitude.Vector)

	// ignore yaw jerk/snap as it causes vibration during heavy rotation (high G-force corners, spin out, etc)
	biasedAttitudeDelta := symmetryaxis.Scale(t.Current.Attitude.Delta, 1.0, 0.25, 1.0)

	t.Current.Attitude.Acceleration = symmetryaxis.Magnitude(biasedAttitudeDelta) / windowMilliseconds

	t.Last.Attitude.Jerk = t.Current.Attitude.Jerk
	t.Current.Attitude.Jerk = (t.Current.Attitude.Acceleration - t.Last.Attitude.Acceleration) / windowMilliseconds
	if t.Current.Attitude.Jerk > 10 {
		t.Current.Attitude.Jerk = 10
	} else if t.Current.Attitude.Jerk < -10 {
		t.Current.Attitude.Jerk = -10
	}

	t.Last.Attitude.Snap = t.Current.Attitude.Snap
	t.Current.Attitude.Snap = (t.Current.Attitude.Jerk - t.Last.Attitude.Jerk) / windowMilliseconds

	// chassis position
	t.Current.Velocity.Delta = vector.Delta(t.Current.Velocity.Vector, t.Last.Velocity.Vector)
	t.Current.Velocity.Acceleration = vector.Magnitude(t.Current.Velocity.Delta) / windowMilliseconds

	t.Last.Velocity.Jerk = t.Current.Velocity.Jerk
	t.Current.Velocity.Jerk = (t.Current.Velocity.Acceleration - t.Last.Velocity.Acceleration) / windowMilliseconds

	t.Last.Velocity.Snap = t.Current.Velocity.Snap
	t.Current.Velocity.Snap = (t.Current.Velocity.Jerk - t.Last.Velocity.Jerk) / windowMilliseconds

	t.Last.Velocity.Crackle = t.Current.Velocity.Crackle
	t.Current.Velocity.Crackle = (t.Current.Velocity.Snap - t.Last.Velocity.Snap) / windowMilliseconds

	t.Last.TransmissionGear = t.Current.TransmissionGear
	t.Current.TransmissionGear = gtclient.Telemetry.CurrentGear()

	t.Last.SequenceID = t.Current.SequenceID
}
