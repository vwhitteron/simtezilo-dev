// capture.go drives the real chassis impact-pulse generator (app/haptics)
// against a recorded telemetry replay and returns the resulting synth output as
// plain samples, with a per-telemetry-frame index that maps each frame back to
// its position in the sample stream. The continuous road-texture layer is
// deliberately not driven, so the capture is the bump haptics alone.
//
// It is deliberately free of any audio-backend (portaudio/CGO) dependency: it
// builds a real synthesizer and reads its master output directly, so offline
// tooling (e.g. tools/tune_assistant) can render and audition the chassis haptic
// stream without linking a device backend. The generation itself is the same code
// the live app runs — there is no reimplementation of the haptic chain here.
//
// The frame loop mirrors tools/tune_assistant's collectAllLaps exactly (skip frames
// before telemetry starts; index each lap from zero), so a Frame's Lap/FrameIndex
// line up one-for-one with that tool's map/speed tracks and the audio window can be
// sliced to a visible section by frame range.
package haptics

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
)

// telemetryFrameRate (defined in generator.go) mirrors app.telemetryFrameRate: the
// GT packet rate the live app refreshes the chassis haptic at (one bump
// regeneration per delivered packet).

// Tuning overrides the four jerk/snap generator knobs the web UI exposes. A zero
// value for any field leaves the shipped default in place. Amplitude is driven by
// the jerk pair (curve + max), frequency by the snap pair; the config derives the
// internal jerk/snap scale factors from these on set, exactly as the live app does.
type Tuning struct {
	JerkCurve int // GetHapticsJerkCurve — amplitude response curvature
	JerkMax   int // GetHapticsJerkMax — amplitude scale ceiling
	SnapCurve int // GetHapticsSnapCurve — frequency response curvature
	SnapMax   int // GetHapticsSnapMax — frequency scale ceiling
}

// DefaultTuning returns the shipped default jerk/snap knob values, read from a fresh
// default config so the UI's sliders start where the live app does and stay in step
// with config_default.go.
func DefaultTuning() Tuning {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	return Tuning{
		JerkCurve: int(cfg.GethapticsJerkCurve()),
		JerkMax:   cfg.GetHapticsJerkMax(),
		SnapCurve: int(cfg.GetHapticsSnapCurve()),
		SnapMax:   cfg.GetHapticsSnapMax(),
	}
}

// CaptureOptions configures a chassis capture run.
type CaptureOptions struct {
	Source     string // file://... replay URL
	Tuning     Tuning // generator overrides (zero fields keep defaults)
	Unfiltered bool   // bypass the kinematics fs/2 nyquist gate (raw ungated render)
}

// Frame records a single chassis refresh: where in the sample stream it starts, the
// lap and per-lap frame index (so the caller can align samples to a lap's map/speed
// tracks), and the jerk/snap that drove the pulse plus the resulting pulse metrics.
type Frame struct {
	OutCursor  int     `json:"outCursor"`  // sample index in Samples where this frame's audio begins
	Lap        int16   `json:"lap"`        // telemetry lap number
	FrameIndex int     `json:"frameIndex"` // index of this frame within its lap
	Seq        uint32  `json:"seq"`        // telemetry sequence number
	Jerk       float64 `json:"jerk"`       // calculated translational jerk magnitude
	Snap       float64 `json:"snap"`       // calculated translational snap magnitude
	Amplitude  float64 `json:"amplitude"`  // resulting pulse amplitude (channel 0)
	FreqHz     float64 `json:"freqHz"`     // resulting pulse frequency (channel 0)
}

// Capture is the result of a run: the mono chassis-pulse master output and the
// per-frame index into it.
type Capture struct {
	InternalRate int       // synth internal sample rate (Hz) of Samples
	Samples      []float64 // mono master output
	Frames       []Frame
}

// CaptureChassis renders the chassis haptic stream for the whole replay. It
// builds a real synthesizer, drives the real haptics generators per delivered
// telemetry packet (matching the live refresh cadence) and returns the
// captured output plus the frame index.
func CaptureChassis(opts CaptureOptions) (*Capture, error) {
	if opts.Source == "" {
		return nil, errors.New("a replay Source is required")
	}

	logger := zerolog.New(io.Discard)

	cfg := config.New(config.Options{Logger: logger})
	applyTuning(cfg, opts.Tuning)

	calib, err := calibrator.NewToneGenerator(cfg)
	if err != nil {
		return nil, fmt.Errorf("calibrator: %w", err)
	}

	var kin kinematics.State
	kin.DisableNyquistGate = opts.Unfiltered

	synth, err := synthesizer.New(&synthesizer.SynthOpts{
		Config:     cfg.GetSynthesizer(),
		BaseConfig: cfg,
		Logger:     logger,
		Kinematics: &kin,
		Calibrator: calib,
	})
	if err != nil {
		return nil, fmt.Errorf("synth: %w", err)
	}

	gen := NewGenerator(cfg, synth, &kin, logger)

	client, err := gttelemetry.New(gttelemetry.Options{Source: opts.Source, Logger: &logger})
	if err != nil {
		return nil, fmt.Errorf("telemetry client: %w", err)
	}

	internalRate := synth.GetSampleRate()
	out := &Capture{InternalRate: internalRate}

	readBuf := make([]float64, 512)

	// Fractional samples-per-frame accumulator so the total tracks real duration even
	// though internalRate is not an integer multiple of telemetryFrameRate.
	samplesPerFrame := float64(internalRate) / float64(telemetryFrameRate)

	var (
		dims            vehicle.Dimensions
		vehicleCaptured bool
		chassisLastSeq  uint32
		lapFrameIndex   = make(map[int16]int)
		cursor          int
		frameCarry      float64
	)

	ctx := context.Background()

	for frame, scanErr := range client.Scan(ctx) {
		if scanErr != nil {
			return nil, fmt.Errorf("reading frame: %w", scanErr)
		}

		if !frame.TelemetryStarted() {
			continue
		}

		if !vehicleCaptured {
			dims = captureDimensions(client)
			chassisLastSeq = frame.SequenceID()
			vehicleCaptured = true
		}

		seq := frame.SequenceID()

		// The kinematics window spans the sequence delta, so a dropped packet widens it
		// and reshapes jerk/snap exactly as the live app experiences it.
		delta := uint32(int64(seq) - int64(chassisLastSeq))
		if delta == 0 {
			delta = 1
		}

		kin.Update(float64(delta)/float64(telemetryFrameRate), dims, client)

		// Only the chassis impact pulse is rendered — not the continuous road-texture
		// layer (gen.Texture()), which would otherwise dominate the capture as broadband
		// road noise. This tool auditions the bump haptics in isolation.
		gen.Chassis()

		lap := frame.CurrentLap()
		idx := lapFrameIndex[lap]
		lapFrameIndex[lap] = idx + 1

		out.Frames = append(out.Frames, Frame{
			OutCursor:  cursor,
			Lap:        lap,
			FrameIndex: idx,
			Seq:        seq,
			Jerk:       kin.Current.SixDOFTranslationCalc.Jerk,
			Snap:       kin.Current.SixDOFTranslationCalc.Snap,
			Amplitude:  channelValueAt(kin.Current.SynthChannelAmplitude, 0),
			FreqHz:     channelValueAt(kin.Current.SynthChannelFrequency, 0),
		})

		chassisLastSeq = seq

		// Drain this frame's worth of master output, tracking the fractional remainder.
		frameCarry += samplesPerFrame
		want := int(frameCarry)
		frameCarry -= float64(want)

		for want > 0 {
			n := synth.ReadBuffer(readBuf[:min(want, len(readBuf))])
			if n == 0 {
				break
			}

			out.Samples = append(out.Samples, readBuf[:n]...)
			cursor += n
			want -= n
		}
	}

	return out, nil
}

// applyTuning writes the non-zero override knobs into the config. Setting the
// curve/max pairs recomputes the derived jerk/snap scale factors internally.
func applyTuning(cfg *config.Config, t Tuning) {
	if t.JerkCurve > 0 {
		cfg.SetHapticsJerkCurve(t.JerkCurve)
	}

	if t.JerkMax > 0 {
		cfg.SetHapticsJerkMax(t.JerkMax)
	}

	if t.SnapCurve > 0 {
		cfg.SetHapticsSnapCurve(t.SnapCurve)
	}

	if t.SnapMax > 0 {
		cfg.SetHapticsSnapMax(t.SnapMax)
	}
}

// captureDimensions reproduces the app's wheelbase/track derivation, which the
// chassis path needs (kinematics scales rotational velocity by these radii). It
// mirrors tune_assistant's collectAllLaps so both derive identical dimensions.
func captureDimensions(client *gttelemetry.Client) vehicle.Dimensions {
	t := client.Telemetry

	var wheelbaseMetres float32
	if wb := t.VehicleWheelbaseMillimetres(); wb > 0 {
		wheelbaseMetres = float32(wb) / 1000
	} else {
		wheelbaseMetres = (float32(t.VehicleLengthMillimetres()) / 1000) * 0.55
	}

	var trackFrontMetres, trackRearMetres float32
	if tf, tr := t.VehicleTrackFrontMillimetres(), t.VehicleTrackRearMillimetres(); tf > 0 || tr > 0 {
		trackFrontMetres = float32(tf) / 1000
		trackRearMetres = float32(tr) / 1000
	} else {
		trackFrontMetres = (float32(t.VehicleWidthMillimetres()) / 1000) * 0.85
		trackRearMetres = trackFrontMetres
	}

	trackWidthMetres := (trackFrontMetres + trackRearMetres) / 2

	return vehicle.Dimensions{
		WheelbaseMetres:    wheelbaseMetres,
		TrackWidthMetres:   trackWidthMetres,
		LongitudinalRadius: wheelbaseMetres / 2,
		TransverseRadius:   trackWidthMetres / 2,
	}
}

// channelValueAt returns values[index], or 0 when index is out of range.
func channelValueAt(values []float64, index int) float64 {
	if index < 0 || index >= len(values) {
		return 0
	}

	return values[index]
}
