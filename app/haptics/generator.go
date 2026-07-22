// Package haptics holds the chassis impact-pulse and road-texture haptic
// generators. It is deliberately free of any audio-backend (portaudio/CGO)
// dependency so that offline tooling can drive the real generators against
// recorded telemetry without linking a device backend.
//
// The generators were extracted verbatim from package app; App now delegates to
// a Generator so there is a single source of truth for the chassis haptic chain.
// A Generator holds exactly the state the two generators need — the config, the
// synthesizer they write into, a pointer to the shared kinematics state they read
// from, and the per-tick scratch/filter state — and nothing else.
package haptics

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// These mirror the identically-named constants in package app. They are copied
// (not imported) because package app imports haptics, so haptics cannot import
// back into app without a cycle. Keep them in step with app/app_constants.go.
const (
	hapticFrameRate    = 120 // 120 Hz — chassis pulse minimum-frame sizing
	telemetryFrameRate = 60  // 60 Hz — texture cushion sizing
)

// Generator produces the chassis impact pulse and the road-texture layer. It is
// created once per synthesizer and driven one frame at a time on a single
// goroutine (the app main loop, or an offline capture driver); its scratch and
// filter fields are not safe for concurrent use.
type Generator struct {
	cfg   *config.Config
	synth *synthesizer.Synthesizer
	kin   *kinematics.State
	log   zerolog.Logger

	// Chassis impact-pulse state.
	jerkPeakHold         float64       //nolint:unused // peak-hold for planned inverse-jerk detection; deliberately kept
	jerkPeakHoldTime     time.Time     //nolint:unused // peak-hold for planned inverse-jerk detection; deliberately kept
	jerkPeakHoldDuration time.Duration // Duration to hold peak based on pulse length
	chassisFreqSmoothed  float64       // Asymmetric follower state for the chassis pulse frequency
	chassisPulseScratch  []float64     // Reusable per-tick pulse buffer for Chassis

	// Road-texture layer state.
	textureState   []textureChannelState // Per-channel noise/filter state
	textureScratch []float64             // Reusable per-tick sample buffer for Texture
}

// NewGenerator builds a Generator bound to the given config, synthesizer and
// kinematics state. The kinematics pointer must remain valid for the
// Generator's lifetime; the generators read the caller's live
// kinematics.Current each frame.
func NewGenerator(cfg *config.Config, synth *synthesizer.Synthesizer, kin *kinematics.State, logger zerolog.Logger) *Generator {
	return &Generator{cfg: cfg, synth: synth, kin: kin, log: logger}
}

// Reset clears the per-session generator state: the chassis pulse-frequency
// follower and the texture noise/filter state, so a new session starts from
// silence rather than inheriting a stale glide value or filter history.
func (g *Generator) Reset() {
	g.chassisFreqSmoothed = 0
	g.jerkPeakHold = 0
	g.jerkPeakHoldTime = time.Time{}

	for i := range g.textureState {
		g.textureState[i] = textureChannelState{}
	}
}
