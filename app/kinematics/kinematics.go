// Package kinematics provides structures and methods for calculating and tracking vehicle kinematics.
package kinematics

import (
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/kinematics/rotataionalenvelope"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics/translationalenvelope"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
	gttelemetry "github.com/zetetos/gt-telemetry"
	"github.com/zetetos/gt-telemetry/pkg/models"
)

// PositionalDerivatives holds values for the 2nd to 5th derivatives of position.
type PositionalDerivatives struct {
	Acceleration float64
	Jerk         float64
	Snap         float64
	Crackle      float64
}

// CalculatedTranslationalDerivatives holds 6DOF translational derivatives calculated from the telemetry data.
type CalculatedTranslationalDerivatives struct {
	PositionalDerivatives

	Delta    models.Vector
	Velocity models.Vector
}

// RotationalDerivatives holds 6DOF rotational derivatives provided by the telemetry data.
type RotationalDerivatives struct {
	PositionalDerivatives

	Delta    models.RotationalEnvelope
	Velocity models.RotationalEnvelope
}

// TranslationalDerivatives holds 6DOF translational derivatives provided by the telemetry data.
type TranslationalDerivatives struct {
	PositionalDerivatives

	Delta    models.TranslationalEnvelope
	Velocity models.TranslationalEnvelope
}

// Kinematics holds the kinematic state of the vehicle.
type Kinematics struct {
	SequenceID  uint32 // TODO: probably should rely on a.state.current instead of tracking separately here
	ComputeTime time.Duration
	Format      string

	SixDOFTranslationCalc CalculatedTranslationalDerivatives
	SixDOFTranslation     TranslationalDerivatives
	SixDOFRotation        RotationalDerivatives

	TransmissionGear int // TODO: probably should rely on a.state.current instead of tracking separately here

	GroundSpeed     float64
	SurgeCalculated float64

	SynthOutputAmplitude float64
	SynthOutputFrequency int
}

// State tracks the current and previous kinematic states of the vehicle.
type State struct {
	Last    Kinematics
	Current Kinematics
}

// NewKinematicsState creates and initializes a new KinematicsTracker.
func NewKinematicsState() State {
	return State{
		Last:    newKinematics(),
		Current: newKinematics(),
	}
}

// newKinematics initializes a new Kinematics struct with default values.
func newKinematics() Kinematics {
	return Kinematics{
		SixDOFTranslationCalc: CalculatedTranslationalDerivatives{},
		SixDOFTranslation:     TranslationalDerivatives{},
		SixDOFRotation:        RotationalDerivatives{},
		ComputeTime:           0,
		TransmissionGear:      -100,
		GroundSpeed:           0,
		SurgeCalculated:       0,
		SynthOutputAmplitude:  0,
		SynthOutputFrequency:  0,
		Format:                "A",
	}
}

// Update updates the kinematic state based on the provided telemetry data and time window.
// TODO: ideally this should not be given the gt client.
func (k *State) Update(windowSeconds float64, gtclient *gttelemetry.Client) {
	k.Last = k.Current

	k.Current.Format = getTelemetryFormat(gtclient)

	k.Current.SequenceID = gtclient.Telemetry.SequenceID()

	// chassis 3D velocity
	k.Current.SixDOFTranslationCalc.Velocity = gtclient.Telemetry.VelocityVector()
	k.Current.SixDOFTranslationCalc.Delta = vector.Delta(k.Current.SixDOFTranslationCalc.Velocity, k.Last.SixDOFTranslationCalc.Velocity)
	k.Current.SixDOFTranslationCalc.Acceleration = vector.Magnitude(k.Current.SixDOFTranslationCalc.Delta) / windowSeconds
	k.Current.SixDOFTranslationCalc.Jerk = (k.Current.SixDOFTranslationCalc.Acceleration - k.Last.SixDOFTranslationCalc.Acceleration) / windowSeconds
	k.Current.SixDOFTranslationCalc.Snap = (k.Current.SixDOFTranslationCalc.Jerk - k.Last.SixDOFTranslationCalc.Jerk) / windowSeconds
	k.Current.SixDOFTranslationCalc.Crackle = (k.Current.SixDOFTranslationCalc.Snap - k.Last.SixDOFTranslationCalc.Snap) / windowSeconds

	// 6DOF translational envelope
	k.Current.SixDOFTranslation.Velocity = gtclient.Telemetry.TranslationEnvelope()
	k.Current.SixDOFTranslation.Delta = translationalenvelope.Delta(k.Current.SixDOFTranslation.Velocity, k.Last.SixDOFTranslation.Velocity)
	k.Current.SixDOFTranslation.Acceleration = translationalenvelope.Magnitude(k.Current.SixDOFTranslation.Velocity)
	k.Current.SixDOFTranslation.Jerk = (k.Current.SixDOFTranslation.Acceleration - k.Last.SixDOFTranslation.Acceleration) / windowSeconds
	k.Current.SixDOFTranslation.Snap = (k.Current.SixDOFTranslation.Jerk - k.Last.SixDOFTranslation.Jerk) / windowSeconds
	k.Current.SixDOFTranslation.Crackle = (k.Current.SixDOFTranslation.Snap - k.Last.SixDOFTranslation.Snap) / windowSeconds

	// 6DOF rotational envelope
	k.Current.SixDOFRotation.Velocity = gtclient.Telemetry.RotationEnvelope()
	k.Current.SixDOFRotation.Delta = rotataionalenvelope.Delta(k.Current.SixDOFRotation.Velocity, k.Last.SixDOFRotation.Velocity)

	// attenuate yaw jerk and snap as it causes vibration during heavy rotation (high G-force corners, spin out, etc)
	// biasedAttitudeDelta := rotataionalenvelope.Scale(k.Current.SixDOFRotation.Delta, 1.0, 0.25, 1.0)
	// TODO: remove above if non-biased version is acceptable
	biasedAttitudeDelta := k.Current.SixDOFRotation.Delta

	k.Current.SixDOFRotation.Acceleration = rotataionalenvelope.Magnitude(biasedAttitudeDelta) / windowSeconds
	k.Current.SixDOFRotation.Jerk = (k.Current.SixDOFRotation.Acceleration - k.Last.SixDOFRotation.Acceleration) / windowSeconds

	// filter out excessive spikes in jerk
	if k.Current.SixDOFRotation.Jerk > 20 {
		k.Current.SixDOFRotation.Jerk = 20
	} else if k.Current.SixDOFRotation.Jerk < -20 {
		k.Current.SixDOFRotation.Jerk = -20
	}

	k.Current.SixDOFRotation.Snap = (k.Current.SixDOFRotation.Jerk - k.Last.SixDOFRotation.Jerk) / windowSeconds

	k.Current.SurgeCalculated = signal.Abs(float64(k.Current.SixDOFTranslationCalc.Delta.X) / windowSeconds)
	k.Current.GroundSpeed = float64(gtclient.Telemetry.GroundSpeedMetersPerSecond())
	k.Current.TransmissionGear = gtclient.Telemetry.CurrentGear()
}

// GetSurgeGforce calculates and returns the translational envelope surge G-force based on the current kinematic state.
func (k *State) GetSurgeGforce() float64 {
	var surge float64
	if k.Current.Format == "~" || k.Current.Format == "B" {
		surge = float64(k.Current.SixDOFTranslation.Velocity.Surge)
	} else {
		surge = float64(k.Current.SurgeCalculated)
	}

	gForce := signal.Abs(surge / GravityConstant)

	return gForce
}

// getTelemetryFormat determines the telemetry format based on the telemetry client's raw telemetry data.
// Currently supports formats "A", "B", and "~".
func getTelemetryFormat(gtClient *gttelemetry.Client) string {
	isAddendum2, _ := gtClient.Telemetry.RawTelemetry.Addendum2Format()
	if isAddendum2 {
		return "~"
	}

	isAddendum1, _ := gtClient.Telemetry.RawTelemetry.Addendum1Format()
	if isAddendum1 {
		return "B"
	}

	return "A"
}
