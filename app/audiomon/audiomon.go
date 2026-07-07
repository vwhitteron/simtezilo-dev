// Package audiomon implements the haptic audio latency/drift monitor. Telemetry
// arrives at a steady 60 fps carrying a monotonic sequence ID, which gives a
// real-time reference clock. Comparing it against what the audio pipeline has
// actually emitted (and how full the buffers are) surfaces the output-side
// latency that accumulates when sample production outruns the soundcard's
// consumption — the cause of haptics drifting out of sync over time. The math
// helpers are pure so they can be unit-tested.
package audiomon

import (
	"sync"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/audio"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/synthesizer"
)

// driftWindow bounds how long a drift baseline lives before it is re-established.
// The telemetry and soundcard clocks are independent oscillators, so comparing
// them against a fixed baseline integrates their natural crystal skew without
// limit (tens of ppm ≈ a few ms/min climbing forever). Re-baselining over a short
// window makes DriftMs report *recent* divergence instead: it sits near zero in
// steady state and spikes only when the pipeline genuinely falls behind within
// the window, then settles once the baseline rolls forward.
const driftWindow = 5 * time.Second

// Report is a snapshot of haptic-pipeline latency and drift. All
// latencies are milliseconds; Drift is positive when telemetry time has run
// ahead of emitted audio time (the pipeline is lagging behind real time).
type Report struct {
	EngineLatencyMs  float64 // engine mixer-channel buffer depth
	ChassisLatencyMs float64 // chassis channel 0 mixer-channel buffer depth
	RingLatencyMs    float64 // async device ring buffer depth
	DriftMs          float64 // telemetry-clock vs emitted-audio-clock divergence over the recent window
	SeqJitterMs      float64 // smoothed telemetry-cadence jitter (abs deviation from 60 fps)
	Underruns        int64   // ring gap-fills (silence padding) since start
	ProducerWaits    int64   // times the producer blocked on a full ring since start
}

// Monitor owns all haptic audio latency/drift monitor state.
type Monitor struct {
	config        *config.Config // application configuration
	sampleRate    func() int     // synth internal sample rate, read lazily
	telemetryRate int            // telemetry frame rate in Hz (60)

	mu               sync.Mutex // guards fields shared between main loop and web-telemetry goroutine
	driftBaseFrames  int64
	driftBaseSeq     uint32
	driftBaseTime    time.Time
	driftBaseSet     bool
	seqJitterMs      float64
	lastSeqWallClock time.Time
	outputRate       int // actual sink sample rate (may differ from config on native-rate devices)
}

// NewMonitor creates a Monitor. sampleRate is a lazy accessor for the synth's
// internal sample rate, evaluated at call time so the Monitor never holds a
// direct pointer into the synthesizer.
func NewMonitor(cfg *config.Config, sampleRate func() int, telemetryRate int) *Monitor {
	return &Monitor{config: cfg, sampleRate: sampleRate, telemetryRate: telemetryRate}
}

// SetOutputRate records the actual sink sample rate. No lock — matches the
// original unsynchronised field write.
func (m *Monitor) SetOutputRate(rate int) {
	m.outputRate = rate
}

// bufferLatencyMs converts a buffered mono-sample count at the given sample rate
// into milliseconds of audio.
func bufferLatencyMs(usedSamples int, rateHz float64) float64 {
	if rateHz <= 0 {
		return 0
	}

	return float64(usedSamples) / rateHz * 1000
}

// driftMs returns how far the telemetry clock has diverged from the emitted
// audio clock since the (rolling) baseline. audioElapsed comes from frames the
// soundcard callback has pulled (outputRate); telemetryElapsed from sequence IDs
// advanced (telemetryRate, the steady 60 fps). A positive result means audio
// output is lagging behind real time. The baseline is re-established every
// driftWindow, so this reflects recent divergence rather than lifetime crystal
// skew between the two independent clocks.
func driftMs(framesRead, baseFrames int64, seq, baseSeq uint32, outputRate, telemetryRate float64) float64 {
	if outputRate <= 0 || telemetryRate <= 0 {
		return 0
	}

	audioElapsed := float64(framesRead-baseFrames) / outputRate
	telemetryElapsed := float64(seq-baseSeq) / telemetryRate

	return (telemetryElapsed - audioElapsed) * 1000
}

// TrackSequenceJitter updates the smoothed telemetry-cadence jitter: the
// per-packet wall-clock interval's absolute deviation from the expected 60 fps
// period. Called from updateState (main loop) only when the sequence advanced,
// so a non-zero value flags telemetry-side stalls rather than audio drift.
func (m *Monitor) TrackSequenceJitter(delta uint32) {
	now := time.Now()

	if !m.lastSeqWallClock.IsZero() && delta > 0 {
		perPacket := now.Sub(m.lastSeqWallClock) / time.Duration(delta)

		deviation := perPacket - time.Second/time.Duration(m.telemetryRate)
		if deviation < 0 {
			deviation = -deviation
		}

		deviationMs := float64(deviation.Microseconds()) / 1000

		m.mu.Lock()

		if m.seqJitterMs == 0 {
			m.seqJitterMs = deviationMs
		} else {
			// Light EWMA to smooth single-packet scheduling noise.
			m.seqJitterMs = 0.9*m.seqJitterMs + 0.1*deviationMs
		}

		m.mu.Unlock()
	}

	m.lastSeqWallClock = now
}

// ResetBaseline clears the drift baseline so the next report re-establishes
// it. Call this whenever the device frame counter restarts (a new async ring),
// e.g. on audio output start/restart.
func (m *Monitor) ResetBaseline() {
	m.mu.Lock()
	m.driftBaseSet = false
	m.mu.Unlock()
}

// BuildReport derives the latency/drift snapshot from an async-ring health
// snapshot and the mixer channel diagnostics (both already gathered by the
// caller). It lazily establishes the drift baseline and reads the smoothed
// jitter under mu, so it is safe to call from the web-telemetry goroutine as
// well as the main loop.
func (m *Monitor) BuildReport(health audio.HealthMetrics, diag synthesizer.MixerDiagnostics, seq uint32) Report {
	internalRate := float64(m.sampleRate())

	// Prefer the rate the sink actually opened at; it can differ from the config
	// when a device's native rate is used (Bluetooth, etc.). Fall back to config
	// before the first audio start.
	outputRate := float64(m.outputRate)
	if outputRate <= 0 {
		outputRate = float64(m.config.GetAudioHapticsSampleRate())
	}

	channels := m.config.GetAudioHapticsChannels()

	channelUsed := func(name string) int {
		for i := range diag.Channels {
			if diag.Channels[i].Name == name {
				return diag.Channels[i].Health.Used
			}
		}

		return 0
	}

	report := Report{
		EngineLatencyMs:  bufferLatencyMs(channelUsed(synthesizer.ChannelEngine), internalRate),
		ChassisLatencyMs: bufferLatencyMs(channelUsed(synthesizer.ChassisChannelName(0)), internalRate),
		Underruns:        health.Underruns,
		ProducerWaits:    health.ProducerWaits,
	}

	if channels > 0 {
		// RingUsed counts interleaved sample slots; divide by channels for frames.
		report.RingLatencyMs = bufferLatencyMs(health.RingUsed/channels, outputRate)
	}

	m.mu.Lock()
	report.SeqJitterMs = m.seqJitterMs

	if !m.driftBaseSet {
		m.driftBaseFrames = health.FramesRead
		m.driftBaseSeq = seq
		m.driftBaseTime = time.Now()
		m.driftBaseSet = true
	} else {
		report.DriftMs = driftMs(
			health.FramesRead, m.driftBaseFrames,
			seq, m.driftBaseSeq,
			outputRate, float64(m.telemetryRate),
		)

		// Roll the baseline forward once the window elapses so DriftMs measures
		// divergence over the recent window rather than integrating clock skew
		// for the whole session.
		if time.Since(m.driftBaseTime) >= driftWindow {
			m.driftBaseFrames = health.FramesRead
			m.driftBaseSeq = seq
			m.driftBaseTime = time.Now()
		}
	}

	m.mu.Unlock()

	return report
}
