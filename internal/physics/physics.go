package physics

import (
	"time"

	"github.com/vwhitteron/simtezilo-dev/internal/physics/symmetryaxis"
	"github.com/vwhitteron/simtezilo-dev/internal/physics/translationalenvelope"
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

type RotationalDerivatives struct {
	Delta          telemetry_client.SymmetryAxes
	Vector         telemetry_client.SymmetryAxes
	Acceleration3D float64
	Jerk           float64
	Snap           float64
	Crackle        float64
}

type TranslationalDerivatives struct {
	Delta     telemetry_client.TranslationalEnvelope
	Vector    telemetry_client.TranslationalEnvelope
	Magnitude float64
	Jerk      float64
	Snap      float64
	Crackle   float64
}

type Physics struct {
	SequenceID  uint32
	ComputeTime time.Duration

	TranslationalEnvelope TranslationalDerivatives
	RotationalEnvelope    RotationalDerivatives
	Velocity              VelocityDerivatives

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
		TranslationalEnvelope: TranslationalDerivatives{},
		RotationalEnvelope: RotationalDerivatives{
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
func (t *PhysicsTracker) Update(windowSeconds float64, gtclient *telemetry_client.GTClient) {
	t.Last = t.Current

	// t.Last.SequenceID = t.Current.SequenceID

	t.Current.SequenceID = gtclient.Telemetry.SequenceID()
	t.Current.Velocity.Vector = gtclient.Telemetry.VelocityVector()
	t.Current.RotationalEnvelope.Vector = gtclient.Telemetry.RotationVector()

	// chassis longitudinal velocity
	t.Current.GroundSpeed = gtclient.Telemetry.GroundSpeedMetersPerSecond()
	t.Current.AccelerationLongitude = (t.Current.GroundSpeed - t.Last.GroundSpeed) / float32(windowSeconds)

	// chassis rotational envelope
	t.Current.RotationalEnvelope.Delta = symmetryaxis.Delta(t.Current.RotationalEnvelope.Vector, t.Last.RotationalEnvelope.Vector)

	// attenuate yaw jerk/snap as it causes vibration during heavy rotation (high G-force corners, spin out, etc)
	biasedAttitudeDelta := symmetryaxis.Scale(t.Current.RotationalEnvelope.Delta, 1.0, 0.25, 1.0)

	t.Current.RotationalEnvelope.Acceleration3D = symmetryaxis.Magnitude(biasedAttitudeDelta) / windowSeconds

	t.Current.RotationalEnvelope.Jerk = (t.Current.RotationalEnvelope.Acceleration3D - t.Last.RotationalEnvelope.Acceleration3D) / windowSeconds
	if t.Current.RotationalEnvelope.Jerk > 10 {
		t.Current.RotationalEnvelope.Jerk = 10
	} else if t.Current.RotationalEnvelope.Jerk < -10 {
		t.Current.RotationalEnvelope.Jerk = -10
	}

	t.Current.RotationalEnvelope.Snap = (t.Current.RotationalEnvelope.Jerk - t.Last.RotationalEnvelope.Jerk) / windowSeconds

	// chassis 3D velocity
	t.Current.Velocity.Delta = vector.Delta(t.Current.Velocity.Vector, t.Last.Velocity.Vector)
	t.Current.Velocity.Acceleration3D = vector.Magnitude(t.Current.Velocity.Delta) / windowSeconds
	t.Current.Velocity.Jerk = (t.Current.Velocity.Acceleration3D - t.Last.Velocity.Acceleration3D) / windowSeconds
	t.Current.Velocity.Snap = (t.Current.Velocity.Jerk - t.Last.Velocity.Jerk) / windowSeconds
	t.Current.Velocity.Crackle = (t.Current.Velocity.Snap - t.Last.Velocity.Snap) / windowSeconds

	// 6DOF translational envelope
	t.Current.TranslationalEnvelope.Vector = gtclient.Telemetry.TranslationEnvelope()
	t.Current.TranslationalEnvelope.Delta = translationalenvelope.Delta(t.Current.TranslationalEnvelope.Vector, t.Last.TranslationalEnvelope.Vector)
	t.Current.TranslationalEnvelope.Magnitude = translationalenvelope.Magnitude(t.Current.TranslationalEnvelope.Vector)
	t.Current.TranslationalEnvelope.Jerk = (t.Current.TranslationalEnvelope.Magnitude - t.Last.TranslationalEnvelope.Magnitude) / windowSeconds
	t.Current.TranslationalEnvelope.Snap = (t.Current.TranslationalEnvelope.Jerk - t.Last.TranslationalEnvelope.Jerk) / windowSeconds
	t.Current.TranslationalEnvelope.Crackle = (t.Current.TranslationalEnvelope.Snap - t.Last.TranslationalEnvelope.Snap) / windowSeconds

	t.Current.TransmissionGear = gtclient.Telemetry.CurrentGear()
}
