package app

// Haptic capture harness. This is a diagnostic entry point (not used by the running
// app) that drives the REAL haptic generators against recorded telemetry and
// captures the resulting synth output for artifact analysis. Two paths are covered:
//
//   - engine: generateEngineHaptic, which regenerates a pulse waveform every
//     engine-frame and stitches it into the live buffer at a zero crossing
//     (the buffer-write-boundary "seam").
//   - chassis ("bump"): kinematics.Update + generateChassisHaptic, whose pulse
//     amplitude/frequency are driven by jerk/snap derived from telemetry over a
//     window of sequenceDelta/packetRate seconds — so a dropped packet widens that
//     window and can distort the jerk/snap that shapes the bump.
//
// It runs a single-threaded discrete-event simulation rather than the app's
// real-time tickers: engine haptics fire at engineHapticFrameRate, chassis haptics
// fire per delivered telemetry packet (the rate at which the live app refreshes
// them), packets advance at the GT packet rate, and between writes the synth master
// buffer is drained at the internal sample rate. That reproduces the real relative
// timing of writes versus reads (which is what determines the seam) deterministically
// and as fast as the machine allows. It lives in package app because the generators
// it exercises are unexported; all analysis/reporting is done by the caller.
//
// Dropped packets are not injected: they are already present in the recording as
// sequence-ID gaps, and are reproduced faithfully (getCurrentRPM's cached-RPM
// fallback for engine; the widened kinematics window for chassis). No production
// code is modified.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
)

// HapticCaptureOptions configures a capture run.
type HapticCaptureOptions struct {
	Source      string  // file://... replay URL (provides the vehicle and telemetry trace)
	SeekSeconds float64 // skip this many seconds of replay before capturing
	DurSeconds  float64 // capture this many seconds of replay (<=0 means to end)
	Engine      bool    // drive the engine haptic
	Chassis     bool    // drive the chassis ("bump") haptic
}

// EngineFrame records the per-engine-tick context, so the caller can correlate a
// captured discontinuity back to the telemetry that produced it.
type EngineFrame struct {
	OutCursor int     // engine master-output sample index reached at this tick
	Seq       uint32  // telemetry sequence number in effect
	RPM       float64 // RPM used for this tick's waveform
	Dropped   int     // packets missing since the previous tick (sequence gap)
	Cached    bool    // true if RPM came from the cached fallback (no fresh packet)
}

// ChassisFrame records the per-packet bump context: the jerk/snap that drove the
// pulse and the resulting amplitude/frequency, plus how many packets the window
// spanned (>1 means the window was widened by a drop).
type ChassisFrame struct {
	OutCursor int
	Seq       uint32
	Delta     uint32  // packets the kinematics window spanned (1 = no drop)
	Jerk      float64 // calculated translational jerk magnitude
	Snap      float64 // calculated translational snap magnitude
	Amplitude float64 // resulting pulse amplitude (channel 0)
	FreqHz    float64 // resulting pulse frequency (channel 0)
}

// HapticCapture is the result of a capture run. Samples is the combined master
// output of whichever haptics were enabled.
type HapticCapture struct {
	InternalRate    int
	EngineFrameRate int
	Samples         []float64
	EngineFrames    []EngineFrame
	ChassisFrames   []ChassisFrame
	VehicleID       uint32
	Geometry        string
	RevLimit        uint16
	FiringFrequency float64
}

// CaptureHaptics drives the selected real haptic generators against a telemetry
// replay and returns the captured synth output plus per-frame telemetry context.
func CaptureHaptics(opts HapticCaptureOptions) (*HapticCapture, error) {
	if opts.Source == "" {
		return nil, errors.New("a replay Source is required")
	}

	if !opts.Engine && !opts.Chassis {
		return nil, errors.New("enable at least one of Engine or Chassis")
	}

	app, client, err := newCaptureApp(opts.Source)
	if err != nil {
		return nil, err
	}

	// The mixer's background goroutines keep it and its channel buffers reachable
	// until closed, so a repeated caller would accumulate a dead synth per capture.
	defer func() { _ = app.synth.Close() }()

	ctx := context.Background()
	next, stop := iter.Pull2(client.Scan(ctx))

	defer stop()

	// Advance to the first live frame that carries a vehicle, honouring the seek.
	err = app.seekToVehicle(next, opts.SeekSeconds)
	if err != nil {
		return nil, err
	}

	app.buildVehicleForCapture()
	app.state.telemetryActive = true

	return app.runCaptureLoop(next, opts), nil
}

// newCaptureApp builds the minimal App needed by the haptic paths: config
// (defaults), calibrator, synthesizer and the replay telemetry client. It
// deliberately leaves UI, hardware, channels and background tasks nil — the haptic
// generators never touch them.
func newCaptureApp(source string) (*App, *gttelemetry.Client, error) {
	logger := zerolog.New(io.Discard)

	cfg := config.New(config.Options{Logger: logger})

	calib, err := calibrator.NewToneGenerator(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("calibrator: %w", err)
	}

	app := &App{
		log:        logger,
		config:     cfg,
		calibrator: calib,
		state:      NewGameState(&logger),
	}

	app.synth, err = synthesizer.New(&synthesizer.SynthOpts{
		Config:     cfg.GetSynthesizer(),
		BaseConfig: cfg,
		Logger:     logger,
		Kinematics: &app.kinematics,
		Calibrator: calib,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("synth: %w", err)
	}

	app.chassisGen = haptics.NewGenerator(app.config, app.synth, &app.kinematics, logger)

	client, err := gttelemetry.New(gttelemetry.Options{Source: source, Logger: &logger})
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry client: %w", err)
	}

	app.gtClient = client

	return app, client, nil
}

// pullFunc is the pull-iterator signature returned by iter.Pull2 over Scan.
type pullFunc = func() (*gttelemetry.Transformer, error, bool)

// seekToVehicle advances the replay past the seek window and stops on the first
// frame that exposes an engine layout, applying that telemetry to app state.
func (a *App) seekToVehicle(next pullFunc, seekSeconds float64) error {
	skip := int(seekSeconds * float64(telemetryFrameRate))

	seen := 0

	for {
		frame, scanErr, ok := next()
		if scanErr != nil {
			return fmt.Errorf("scan: %w", scanErr)
		}

		if !ok {
			return errors.New("reached end of replay before a vehicle/engine appeared")
		}

		seen++

		a.state.current.sequenceNumber = frame.SequenceID()

		if seen >= skip && frame.VehicleEngineLayout() != "" {
			return nil
		}
	}
}

// buildVehicleForCapture assembles a.vehicle from the current telemetry frame,
// reusing the real engine-characteristics logic but skipping updateVehicle's UI and
// odometer/fuel side effects (which the capture App has no state for).
func (a *App) buildVehicleForCapture() {
	vehicleType := vehicle.DetermineVehicleType(a.gtClient.Telemetry.VehicleType())
	engine := a.getEngineData()
	revLimit := a.gtClient.Telemetry.EngineRPMLight().Max

	a.adjustEngineHaptics(&engine, revLimit)

	a.vehicle = vehicle.Characteristics{
		ID:          a.gtClient.Telemetry.VehicleID(),
		VehicleType: vehicleType,
		Engine:      engine,
		RevLimit:    a.normalizeRevLimit(revLimit),
		Dimensions:  a.captureDimensions(),
	}

	a.setTransmissionGain(vehicleType)
}

// captureDimensions reproduces updateVehicle's wheelbase/track derivation, which
// the chassis path needs (kinematics scales rotational velocity by these radii).
func (a *App) captureDimensions() vehicle.Dimensions {
	client := a.gtClient.Telemetry

	var wheelbaseMetres float32
	if wb := client.VehicleWheelbaseMillimetres(); wb > 0 {
		wheelbaseMetres = float32(wb) / 1000
	} else {
		wheelbaseMetres = (float32(client.VehicleLengthMillimetres()) / 1000) * 0.55
	}

	var trackFrontMetres, trackRearMetres float32
	if tf, tr := client.VehicleTrackFrontMillimetres(), client.VehicleTrackRearMillimetres(); tf > 0 || tr > 0 {
		trackFrontMetres = float32(tf) / 1000
		trackRearMetres = float32(tr) / 1000
	} else {
		trackFrontMetres = (float32(client.VehicleWidthMillimetres()) / 1000) * 0.85
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

// runCaptureLoop is the discrete-event simulation. It interleaves packet arrivals
// (telemetryFrameRate), chassis-haptic refreshes (one per delivered packet),
// engine-haptic generation (engineHapticFrameRate) and master-buffer draining
// (InternalRate) on a single simulated clock.
func (a *App) runCaptureLoop(next pullFunc, opts HapticCaptureOptions) *HapticCapture {
	internalRate := a.synth.GetSampleRate()

	out := &HapticCapture{
		InternalRate:    internalRate,
		EngineFrameRate: engineHapticFrameRate,
		VehicleID:       a.vehicle.ID,
		Geometry:        a.vehicle.Engine.Geometry,
		RevLimit:        a.vehicle.RevLimit,
		FiringFrequency: a.vehicle.Engine.FiringFrequency,
	}

	const chunkFrames = 64

	readBuf := make([]float64, chunkFrames)

	chunkDur := float64(chunkFrames) / float64(internalRate)
	packetPeriod := 1.0 / float64(telemetryFrameRate)
	enginePeriod := 1.0 / float64(engineHapticFrameRate)

	var (
		simTime        float64
		nextPacket     float64
		nextEngine     float64
		cursor         int
		prevSeq        = a.state.current.sequenceNumber
		chassisLastSeq = a.state.current.sequenceNumber
		dropsPending   int // packet-granularity drops seen since the last engine tick
		ended          bool
	)

	for (opts.DurSeconds <= 0 || simTime < opts.DurSeconds) && !ended {
		// Deliver any telemetry packets due by now. Drops are counted at packet
		// granularity (a sequence gap between consecutive packets), not across the
		// wider engine-tick interval, which normally spans two packets.
		for nextPacket <= simTime {
			frame, _, ok := next()
			if !ok {
				ended = true

				break
			}

			seq := frame.SequenceID()
			if gap := int(int64(seq)-int64(prevSeq)) - 1; gap > 0 {
				dropsPending += gap
			}

			prevSeq = seq
			a.state.current.sequenceNumber = seq
			nextPacket += packetPeriod

			// Chassis refreshes once per delivered packet, exactly as the live app
			// regenerates the bump on each new telemetry frame. The kinematics window
			// spans the sequence delta, so a drop widens it (and reshapes jerk/snap).
			if opts.Chassis {
				delta := uint32(int64(seq) - int64(chassisLastSeq))
				if delta == 0 {
					delta = 1
				}

				a.kinematics.Update(float64(delta)/float64(telemetryFrameRate), a.vehicle.Dimensions, a.gtClient)
				a.chassisGen.Chassis()
				a.chassisGen.Texture()

				out.ChassisFrames = append(out.ChassisFrames, ChassisFrame{
					OutCursor: cursor,
					Seq:       seq,
					Delta:     delta,
					Jerk:      a.kinematics.Current.SixDOFTranslationCalc.Jerk,
					Snap:      a.kinematics.Current.SixDOFTranslationCalc.Snap,
					Amplitude: channelValueAt(a.kinematics.Current.SynthChannelAmplitude, 0),
					FreqHz:    channelValueAt(a.kinematics.Current.SynthChannelFrequency, 0),
				})

				chassisLastSeq = seq
			}
		}

		// Fire any engine-haptic ticks due by now.
		for nextEngine <= simTime {
			if !opts.Engine {
				nextEngine += enginePeriod

				continue
			}

			seq := a.state.current.sequenceNumber
			// "Cached" means no fresh packet has advanced the sequence since the
			// engine generator last consumed one, so getCurrentRPM falls back to the
			// held RPM (the real effect of a drop landing on this tick).
			cached := seq <= a.state.engine.lastSeq

			a.generateEngineHaptic()

			out.EngineFrames = append(out.EngineFrames, EngineFrame{
				OutCursor: cursor,
				Seq:       seq,
				RPM:       a.state.engine.lastKnownRPM,
				Dropped:   dropsPending,
				Cached:    cached,
			})

			dropsPending = 0
			nextEngine += enginePeriod
		}

		// Drain one chunk of combined master output (only the enabled haptics' channels
		// carry signal; the rest are silent).
		n := a.synth.ReadBuffer(readBuf)
		out.Samples = append(out.Samples, readBuf[:n]...)
		cursor += n

		simTime += chunkDur
	}

	return out
}

// ---------------------------------------------------------------------------
// Real-sink capture: run the actual output pipeline (Streamer -> resample ->
// async ring -> real backend) and tap exactly what the device callback reads.
// ---------------------------------------------------------------------------

// SinkCaptureOptions configures a real-time capture through a real audio backend.
type SinkCaptureOptions struct {
	Source      string
	SeekSeconds float64
	DurSeconds  float64 // wall-clock seconds to capture (this runs in real time)
	Engine      bool
	Chassis     bool

	// Tuning overrides for the underrun experiment. Zero values fall back to the
	// app's configured values, so a default run reproduces the live app exactly.
	LatencyMs          int // overrides the device/ring latency used for sizing
	RingCapacityFrames int // overrides the async ring capacity (frames)
	OutputRate         int // overrides the device output rate (set == internal to skip resampling)
}

// SinkCapture holds the interleaved float32 samples the real device callback
// received, exactly as they crossed the audio abstraction boundary.
type SinkCapture struct {
	OutputRate  int
	Channels    int
	LatencyMs   int       // latency actually used for sizing
	RingFrames  int       // async ring capacity actually used
	BlockFrames int       // producer block size actually used
	Samples     []float32 // interleaved [c0,c1,...]
}

// tapSource wraps the source handed to the sink and copies every sample the device
// callback pulls into a preallocated buffer. It performs no allocation and takes no
// lock on the realtime callback (single writer; read after Stop), so it does not
// perturb the very timing it is measuring.
type tapSource struct {
	inner audio.SampleSource
	rec   []float32
	pos   int
}

// ReadInterleaved implements SampleSource. It copies the requested samples into
// the preallocated rec buffer, advancing pos, and returns the number of frames read
// and whether the source is still producing.
func (t *tapSource) ReadInterleaved(out []float32, channels int) (n int, ok bool) {
	n, ok = t.inner.ReadInterleaved(out, channels)

	if t.pos < len(t.rec) {
		t.pos += copy(t.rec[t.pos:], out[:n*channels])
	}

	return n, ok
}

// CaptureHapticsThroughSink builds the real output pipeline used by the live app
// (Streamer -> resampler -> async ring -> backend sink), drives the haptic
// generators on a single goroutine (mirroring the app's tick loop) for DurSeconds
// of wall-clock time, and returns exactly what the device callback read. This runs
// in real time and opens a real audio device via the backend.
func CaptureHapticsThroughSink(opts SinkCaptureOptions) (*SinkCapture, error) {
	if opts.Source == "" {
		return nil, errors.New("a replay Source is required")
	}

	if !opts.Engine && !opts.Chassis {
		return nil, errors.New("enable at least one of Engine or Chassis")
	}

	if opts.DurSeconds <= 0 {
		return nil, errors.New("DurSeconds must be > 0 for a real-time capture")
	}

	app, client, err := newCaptureApp(opts.Source)
	if err != nil {
		return nil, err
	}

	// See CaptureHaptics: the synth must be closed or its mixer goroutines keep the
	// whole mixer alive past the capture.
	defer func() { _ = app.synth.Close() }()

	ctx := context.Background()
	next, stop := iter.Pull2(client.Scan(ctx))

	defer stop()

	err = app.seekToVehicle(next, opts.SeekSeconds)
	if err != nil {
		return nil, err
	}

	app.buildVehicleForCapture()
	app.state.telemetryActive = true

	return app.captureThroughSink(next, opts)
}

func (a *App) captureThroughSink(next pullFunc, opts SinkCaptureOptions) (*SinkCapture, error) {
	logger := zerolog.New(io.Discard)

	backend, err := audio.New(logger)
	if err != nil {
		return nil, fmt.Errorf("open audio backend: %w", err)
	}

	defer func() { _ = backend.Close() }()

	outputRate := a.config.GetAudioHapticsSampleRate()
	if opts.OutputRate > 0 {
		outputRate = opts.OutputRate
	} else if dev, ok := audio.DefaultOutputDevice(backend); ok && dev.DefaultSampleRate > 0 {
		// The sink opens the system default device (no DeviceID); adopt its native
		// rate so the OS does not insert its own resampling layer.
		outputRate = dev.DefaultSampleRate
	}

	latencyMs := opts.LatencyMs
	if latencyMs <= 0 {
		latencyMs = a.config.GetAudioHapticsLatencyMs()
	}

	sink, err := backend.OpenSink(audio.SinkConfig{
		Channels:   a.config.GetAudioHapticsChannels(),
		SampleRate: outputRate,
		LatencyMs:  latencyMs,
	})
	if err != nil {
		return nil, fmt.Errorf("open sink: %w", err)
	}

	defer func() { _ = sink.Stop() }()

	internalRate := a.synth.GetSampleRate()
	channels := sink.Channels()

	streamer := synthesizer.NewStreamer(a.synth)
	source := audio.NewResamplingSource(streamer, internalRate, outputRate, channels)

	capacity, target, block := hapticBufferFrames(outputRate, latencyMs)
	if opts.RingCapacityFrames > 0 {
		capacity = opts.RingCapacityFrames
	}

	async := audio.NewAsyncSource(source, channels, capacity, target, block)

	defer async.Close()

	// Preallocate the tap buffer for the full duration plus a small tail.
	totalSamples := int((opts.DurSeconds + 0.5) * float64(outputRate) * float64(channels))
	tap := &tapSource{inner: async, rec: make([]float32, totalSamples)}

	err = sink.Start(tap)
	if err != nil {
		return nil, fmt.Errorf("start sink: %w", err)
	}

	// Drive the haptic generators on a single goroutine for the duration, mirroring
	// the app's single tick loop. The device callback is the only other goroutine —
	// the same concurrency the real app has (and the same exposure to the engine
	// offset race).
	a.driveHapticsRealtime(next, opts)

	// Let the ring and device drain the tail before stopping.
	time.Sleep(200 * time.Millisecond)

	return &SinkCapture{
		OutputRate:  outputRate,
		Channels:    channels,
		LatencyMs:   latencyMs,
		RingFrames:  capacity,
		BlockFrames: block,
		Samples:     tap.rec[:tap.pos],
	}, nil
}

// driveHapticsRealtime runs telemetry, chassis and engine generation off a single
// goroutine's tickers for the requested wall-clock duration.
func (a *App) driveHapticsRealtime(next pullFunc, opts SinkCaptureOptions) {
	telemetryTick := time.NewTicker(time.Second / time.Duration(telemetryFrameRate))
	engineTick := time.NewTicker(time.Second / time.Duration(engineHapticFrameRate))

	defer telemetryTick.Stop()
	defer engineTick.Stop()

	deadline := time.After(time.Duration(opts.DurSeconds * float64(time.Second)))

	chassisLastSeq := a.state.current.sequenceNumber

	for {
		select {
		case <-deadline:
			return
		case <-telemetryTick.C:
			tf, _, ok := next()
			if !ok {
				return // end of replay
			}

			seq := tf.SequenceID()
			a.state.current.sequenceNumber = seq

			if opts.Chassis && int64(seq) > int64(chassisLastSeq) {
				delta := uint32(int64(seq) - int64(chassisLastSeq))
				a.kinematics.Update(float64(delta)/float64(telemetryFrameRate), a.vehicle.Dimensions, a.gtClient)
				a.chassisGen.Chassis()
				a.chassisGen.Texture()

				chassisLastSeq = seq
			}
		case <-engineTick.C:
			if opts.Engine {
				a.generateEngineHaptic()
			}
		}
	}
}
