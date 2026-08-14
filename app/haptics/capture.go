// capture.go drives the real chassis impact-pulse generator (app/haptics)
// against a recorded telemetry replay and returns the resulting synth output as
// plain samples, with a per-telemetry-frame index that maps each frame back to
// its position in the sample stream. The continuous road-texture layer is
// deliberately not driven, so the capture is the bump haptics alone.
//
// It is deliberately free of any audio-backend (portaudio/CGO) dependency: it
// builds a real synthesizer and reads its master output directly, so offline
// tooling (e.g. the in-app tuning assistant, app/tuneassist) can render and audition
// the chassis haptic stream without linking a device backend. The generation itself
// is the same code the live app runs — there is no reimplementation of the haptic
// chain here.
//
// The frame loop mirrors app/tuneassist's collectAllLaps exactly (skip frames
// before telemetry starts; index each lap from zero), so a Frame's Lap/FrameIndex
// line up one-for-one with that tool's map/speed tracks and the audio window can be
// sliced to a visible section by frame range.
//
// The package godoc lives in generator.go.

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

// Tuning overrides the jerk/snap generator knobs the web UI exposes. A zero
// value for most fields leaves the shipped default in place. Amplitude is driven by
// the jerk trio (curve, pivot, pivot gain), frequency by the snap pair; the config
// derives the internal jerk/snap scale factors from these on set, exactly as the
// live app does.
type Tuning struct {
	JerkCurve     int     // GetHapticsJerkCurve — amplitude response curvature
	JerkPivot     int     // GetHapticsJerkPivot — reference jerk, in m/s^3
	JerkPivotGain float64 // GetHapticsJerkPivotGain — level of the reference jerk, in dB below full scale
	SnapCurve     int     // GetHapticsSnapCurve — frequency response curvature
	SnapMax       int     // GetHapticsSnapMax — frequency scale ceiling
}

// DefaultTuning returns the shipped default jerk/snap knob values, read from a fresh
// default config so the UI's sliders start where the live app does and stay in step
// with config_default.go.
func DefaultTuning() Tuning {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	return Tuning{
		JerkCurve:     int(cfg.GethapticsJerkCurve()),
		JerkPivot:     cfg.GetHapticsJerkPivot(),
		JerkPivotGain: cfg.GetHapticsJerkPivotGain(),
		SnapCurve:     int(cfg.GetHapticsSnapCurve()),
		SnapMax:       cfg.GetHapticsSnapMax(),
	}
}

// PulseLimits are the shipped chassis pulse bounds: the frequency window the pulse
// frequency is clamped into and the amplitude ceiling. The tune assistant needs them
// to plot gain/frequency, which it derives from jerk/snap client-side rather than
// re-rendering a capture on every slider move.
type PulseLimits struct {
	MinFrequencyHz float64 `json:"minFrequencyHz"`
	MaxFrequencyHz float64 `json:"maxFrequencyHz"`
	MaxAmplitude   float64 `json:"maxAmplitude"`
}

// DefaultPulseLimits reads the pulse bounds from a fresh default config, so they stay
// in step with config_default.go the same way DefaultTuning does.
func DefaultPulseLimits() PulseLimits {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	return PulseLimits{
		MinFrequencyHz: cfg.GetHapticsPulseMinHz(),
		MaxFrequencyHz: cfg.GetHapticsPulseMaxHz(),
		MaxAmplitude:   cfg.GetHapticsPulseMaxAmplitude(),
	}
}

// CaptureWindow restricts which of the rendered samples are handed to a Sink: the
// frames of Lap whose per-lap index falls in [FromFrame, ToFrame]. A negative
// ToFrame runs to the end of the replay. The synthesizer is still driven over the
// whole replay up to that point, since its state (and the kinematics chain) is
// sequential — only the samples outside the window are discarded rather than kept.
type CaptureWindow struct {
	Lap       int16
	FromFrame int
	ToFrame   int
}

// gate reports what the frame at lap/idx means for the window: opens is true once
// the window has been entered (it never re-closes, so frames of other laps falling
// between the bounds are emitted too), and closes is true for the first frame past
// the window, whose samples are the exclusive end and must not be emitted. A nil
// window opens immediately and never closes.
func (w *CaptureWindow) gate(lap int16, idx int) (opens, closes bool) {
	if w == nil {
		return true, false
	}

	if lap != w.Lap {
		return false, false
	}

	if w.ToFrame >= 0 && idx > w.ToFrame {
		return false, true
	}

	return idx >= w.FromFrame, false
}

// CaptureOptions configures a chassis capture run.
type CaptureOptions struct {
	Source     string         // file://... replay URL
	Tuning     Tuning         // generator overrides (zero fields keep defaults)
	Unfiltered bool           // bypass the kinematics fs/2 nyquist gate (raw ungated render)
	Window     *CaptureWindow // restricts Sink delivery to this window; nil means the whole replay

	// Sink, when set, receives each block of master output produced inside the
	// window as it is rendered, and nothing is retained in the returned Capture's
	// Samples/Frames. The slice passed to Sink is only valid for the duration of the
	// call — it is a reused read buffer — so a caller that needs to keep the data
	// must copy it.
	Sink func(samples []float64)
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
// captured output plus the frame index. ctx aborts the frame loop early when
// cancelled.
func CaptureChassis(ctx context.Context, opts CaptureOptions) (*Capture, error) {
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

	// The mixer runs background goroutines rooted in its own context, and those keep
	// the whole mixer — including ~1.3 MB of adaptive channel buffers — reachable for
	// the life of the process. Closing cancels them, so a caller that renders
	// repeatedly (the tuning assistant renders once per audition) does not accumulate
	// a dead synth per render.
	defer func() { _ = synth.Close() }()

	gen := NewGenerator(cfg, synth, &kin, logger)

	client, err := gttelemetry.New(gttelemetry.Options{Source: opts.Source, Logger: &logger})
	if err != nil {
		return nil, fmt.Errorf("telemetry client: %w", err)
	}

	internalRate := synth.GetSampleRate()
	out := &Capture{InternalRate: internalRate}

	route := &captureRouter{out: out, sink: opts.Sink, window: opts.Window}

	readBuf := make([]float64, 512)

	// Fractional samples-per-frame accumulator so the total tracks real duration even
	// though internalRate is not an integer multiple of telemetryFrameRate.
	samplesPerFrame := float64(internalRate) / float64(telemetryFrameRate)

	run := &chassisRun{
		client:          client,
		gen:             gen,
		kin:             &kin,
		route:           route,
		synth:           synth,
		readBuf:         readBuf,
		samplesPerFrame: samplesPerFrame,
		lapFrameIndex:   make(map[int16]int),
	}

	for frame, scanErr := range client.Scan(ctx) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if scanErr != nil {
			return nil, fmt.Errorf("reading frame: %w", scanErr)
		}

		if run.frame(frame) {
			break
		}
	}

	return out, nil
}

// chassisRun holds the mutable per-replay state CaptureChassis advances one telemetry
// frame at a time, so the frame body can live in its own method rather than swelling
// CaptureChassis's branch count.
type chassisRun struct {
	client          *gttelemetry.Client
	gen             *Generator
	kin             *kinematics.State
	route           *captureRouter
	synth           *synthesizer.Synthesizer
	readBuf         []float64
	samplesPerFrame float64

	dims            vehicle.Dimensions
	vehicleCaptured bool
	chassisLastSeq  uint32
	lapFrameIndex   map[int16]int
	frameCarry      float64
}

// frame advances the run by one delivered telemetry packet and reports whether the
// scan should stop, which it does at the first frame past the capture window.
func (r *chassisRun) frame(frame *gttelemetry.Transformer) (stop bool) {
	if !frame.TelemetryStarted() {
		return false
	}

	if !r.vehicleCaptured {
		r.dims = captureDimensions(r.client)
		r.chassisLastSeq = frame.SequenceID()
		r.vehicleCaptured = true
	}

	seq := frame.SequenceID()

	r.kin.Update(float64(frameDelta(seq, r.chassisLastSeq))/float64(telemetryFrameRate), r.dims, r.client)

	// Only the chassis impact pulse is rendered — not the continuous road-texture
	// layer (gen.Texture()), which would otherwise dominate the capture as broadband
	// road noise. This tool auditions the bump haptics in isolation.
	r.gen.Chassis()

	lap := frame.CurrentLap()
	idx := r.lapFrameIndex[lap]
	r.lapFrameIndex[lap] = idx + 1

	if r.route.frame(lap, idx, Frame{
		OutCursor:  r.route.cursor,
		Lap:        lap,
		FrameIndex: idx,
		Seq:        seq,
		Jerk:       r.kin.Current.SixDOFTranslationCalc.Jerk,
		Snap:       r.kin.Current.SixDOFTranslationCalc.Snap,
		Amplitude:  channelValueAt(r.kin.Current.SynthChannelAmplitude, 0),
		FreqHz:     channelValueAt(r.kin.Current.SynthChannelFrequency, 0),
	}) {
		return true
	}

	r.chassisLastSeq = seq

	// Drain this frame's worth of master output, tracking the fractional remainder.
	r.frameCarry += r.samplesPerFrame
	want := int(r.frameCarry)
	r.frameCarry -= float64(want)

	drainFrame(r.synth, r.readBuf, want, r.route.block)

	return false
}

// frameDelta returns the number of telemetry frames spanned since the last one. The
// kinematics window spans this delta, so a dropped packet widens it and reshapes
// jerk/snap exactly as the live app experiences it. A repeated sequence number counts
// as a single frame.
func frameDelta(seq, lastSeq uint32) uint32 {
	// Unsigned subtraction wraps, which is exactly the sequence-number arithmetic
	// wanted here. Do not widen to int64: the conversion back overflows.
	delta := seq - lastSeq
	if delta == 0 {
		return 1
	}

	return delta
}

// captureRouter routes a run's rendered output: a streaming run forwards each block
// to the caller's sink once the window has opened and keeps nothing, while a plain
// run accumulates everything into the Capture.
type captureRouter struct {
	out      *Capture
	sink     func(samples []float64)
	window   *CaptureWindow
	emitting bool
	cursor   int // samples routed so far, i.e. the next frame's OutCursor
}

// frame records a telemetry frame and reports whether the scan should stop, which it
// does at the first frame past the window.
func (r *captureRouter) frame(lap int16, idx int, record Frame) (stop bool) {
	opens, closes := r.window.gate(lap, idx)
	if closes {
		return true
	}

	r.emitting = r.emitting || opens

	if r.sink == nil {
		r.out.Frames = append(r.out.Frames, record)
	}

	return false
}

// block routes one drained block of master output.
func (r *captureRouter) block(samples []float64) {
	switch {
	case r.sink == nil:
		r.out.Samples = append(r.out.Samples, samples...)
	case r.emitting:
		r.sink(samples)
	}

	r.cursor += len(samples)
}

// drainFrame reads up to want samples of master output through readBuf, handing each
// block to consume. It stops early if the synth has no more output to give.
func drainFrame(synth *synthesizer.Synthesizer, readBuf []float64, want int, consume func(samples []float64)) {
	for want > 0 {
		count := synth.ReadBuffer(readBuf[:min(want, len(readBuf))])
		if count == 0 {
			break
		}

		consume(readBuf[:count])

		want -= count
	}
}

// applyTuning writes the non-zero override knobs into the config. Setting the
// curve/max pairs recomputes the derived jerk/snap scale factors internally.
func applyTuning(cfg *config.Config, t Tuning) {
	if t.JerkCurve > 0 {
		cfg.SetHapticsJerkCurve(t.JerkCurve)
	}

	if t.JerkPivot > 0 {
		cfg.SetHapticsJerkPivot(t.JerkPivot)
	}

	// JerkPivotGain's valid range (-12..0 dB) includes negative values and zero,
	// so the usual ">0 means supplied" sentinel can't distinguish "not supplied"
	// from a legitimate 0 dB gain. Use a bounds check instead.
	if t.JerkPivotGain >= -12 && t.JerkPivotGain <= 0 {
		cfg.SetHapticsJerkPivotGain(t.JerkPivotGain)
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
// mirrors app/tuneassist's collectAllLaps so both derive identical dimensions.
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
