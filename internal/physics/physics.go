package physics

import (
	"time"

	"github.com/vwhitteron/simtezilo-dev/internal/physics/symmetryaxis"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/vector"
	telemetry_client "github.com/zetetos/gt-telemetry"
)

type VelocityDerivatives struct {
	Delta          telemetry_client.Vector
	Vector         telemetry_client.Vector
	Acceleration3D float64
	Jerk           float64
	Snap           float64
	Crackle        float64
}

type AttitudeDerivatives struct {
	Delta          telemetry_client.SymmetryAxes
	Vector         telemetry_client.SymmetryAxes
	Acceleration3D float64
	Jerk           float64
	Snap           float64
	Crackle        float64
}

type Physics struct {
	SequenceID  uint32
	ComputeTime time.Duration

	Attitude AttitudeDerivatives
	Velocity VelocityDerivatives

	TransmissionGear int

	GroundSpeed           float32
	AccelerationLongitude float32

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
			Delta:          telemetry_client.SymmetryAxes{},
			Vector:         telemetry_client.SymmetryAxes{},
			Acceleration3D: 0,
			Jerk:           0,
			Snap:           0,
			Crackle:        0,
		},
		ComputeTime: 0,
		Velocity: VelocityDerivatives{
			Delta:          telemetry_client.Vector{},
			Vector:         telemetry_client.Vector{},
			Acceleration3D: 0,
			Jerk:           0,
			Snap:           0,
			Crackle:        0,
		},
		TransmissionGear:      -100,
		GroundSpeed:           0,
		AccelerationLongitude: 0,
		SynthOutputAmplitude:  0,
		SynthOutputFrequency:  0,
	}
}

// TODO: ideally this should not be given the gt client
func (t *PhysicsTracker) Update(windowMilliseconds float64, gtclient *telemetry_client.GTClient) {
	t.Last = t.Current

	// t.Last.SequenceID = t.Current.SequenceID

	t.Current.SequenceID = gtclient.Telemetry.SequenceID()
	t.Current.Velocity.Vector = gtclient.Telemetry.VelocityVector()
	t.Current.Attitude.Vector = gtclient.Telemetry.RotationVector()

	// chassis longitudinal velocity
	t.Current.GroundSpeed = gtclient.Telemetry.GroundSpeedMetersPerSecond()
	t.Current.AccelerationLongitude = (t.Current.GroundSpeed - t.Last.GroundSpeed) / float32(windowMilliseconds)

	// chassis attitude
	t.Current.Attitude.Delta = symmetryaxis.Delta(t.Current.Attitude.Vector, t.Last.Attitude.Vector)

	// attenuate yaw jerk/snap as it causes vibration during heavy rotation (high G-force corners, spin out, etc)
	biasedAttitudeDelta := symmetryaxis.Scale(t.Current.Attitude.Delta, 1.0, 0.25, 1.0)

	t.Current.Attitude.Acceleration3D = symmetryaxis.Magnitude(biasedAttitudeDelta) / windowMilliseconds

	t.Current.Attitude.Jerk = (t.Current.Attitude.Acceleration3D - t.Last.Attitude.Acceleration3D) / windowMilliseconds
	if t.Current.Attitude.Jerk > 10 {
		t.Current.Attitude.Jerk = 10
	} else if t.Current.Attitude.Jerk < -10 {
		t.Current.Attitude.Jerk = -10
	}

	t.Current.Attitude.Snap = (t.Current.Attitude.Jerk - t.Last.Attitude.Jerk) / windowMilliseconds

	// chassis 3D velocity
	t.Current.Velocity.Delta = vector.Delta(t.Current.Velocity.Vector, t.Last.Velocity.Vector)
	t.Current.Velocity.Acceleration3D = vector.Magnitude(t.Current.Velocity.Delta) / windowMilliseconds
	t.Current.Velocity.Jerk = (t.Current.Velocity.Acceleration3D - t.Last.Velocity.Acceleration3D) / windowMilliseconds
	t.Current.Velocity.Snap = (t.Current.Velocity.Jerk - t.Last.Velocity.Jerk) / windowMilliseconds
	t.Current.Velocity.Crackle = (t.Current.Velocity.Snap - t.Last.Velocity.Snap) / windowMilliseconds

	t.Current.TransmissionGear = gtclient.Telemetry.CurrentGear()

}
