package synthesizer

import (
	"math"
	"sync"
	"time"

	"github.com/vwhitteron/simtezilo-dev/app/codec"
)

const (
	effectsSampleRateHz = 32000 // Base sample rate at which sound effects are rendered

	// GearShiftEffectName is the effect the transmission channel plays on a gear
	// change, and the one whose waveform is replaced per vehicle.
	GearShiftEffectName = "gearShift"

	// DefaultGearShiftPulseHz and DefaultGearShiftLengthSeconds are the waveform a
	// vehicle plays before its gearbox has been measured. They sit mid-range between
	// the sharp and heavy ends so an unmeasured car is never badly wrong in either
	// direction.
	DefaultGearShiftPulseHz       = 30.0
	DefaultGearShiftLengthSeconds = 0.1
)

// EffectSample represents a pre-generated audio sample for a sound effect.
type EffectSample struct {
	Name   string
	Sample map[int]codec.PCMFloat64
}

// EffectsSampleBank holds pre-generated audio samples for various sound effects.
type EffectsSampleBank struct {
	// mu guards samples, which is written by GetSample's lazy per-rate resample cache
	// as well as by SetGearShiftPulse. Both the app main loop and the pit radio's
	// background goroutine call GetSample, so the map is genuinely shared.
	mu      sync.RWMutex
	samples map[string]EffectSample
}

// NewEffectsSampleBank initializes and returns a new EffectsSampleBank with pre-generated samples.
func NewEffectsSampleBank() *EffectsSampleBank {
	return &EffectsSampleBank{
		samples: map[string]EffectSample{
			GearShiftEffectName: {
				Name: GearShiftEffectName,
				Sample: map[int]codec.PCMFloat64{
					effectsSampleRateHz: generateGearShiftSample(
						DefaultGearShiftPulseHz, DefaultGearShiftLengthSeconds),
				},
			},
			"talkPermitTone": {
				Name: "talkPermitTone",
				Sample: map[int]codec.PCMFloat64{
					effectsSampleRateHz: generateTalkPermitToneSample(),
				},
			},
			"recordingStartTone": {
				Name: "recordingStartTone",
				Sample: map[int]codec.PCMFloat64{
					effectsSampleRateHz: generateRecordingStartToneSample(),
				},
			},
			"recordingStopTone": {
				Name: "recordingStopTone",
				Sample: map[int]codec.PCMFloat64{
					effectsSampleRateHz: generateRecordingStopToneSample(),
				},
			},
			"errorTone": {
				Name: "errorTone",
				Sample: map[int]codec.PCMFloat64{
					effectsSampleRateHz: generateErrorToneSample(),
				},
			},
		},
	}
}

// GetSample retrieves a pre-generated sample with the given name and sample rate.
// If the sample does not exist, it returns an empty slice.
// The effect is resampled to the requested sample rate if necessary.
func (s *EffectsSampleBank) GetSample(name string, sampleRate int) codec.PCMFloat64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.samples[name]; !ok {
		return codec.PCMFloat64{}
	}

	effect := s.samples[name]

	// Use the cached sample when it exists
	sample, ok := effect.Sample[sampleRate]
	if !ok {
		// Resample and cache the new sample rate when it doesn't exist
		baseSample := effect.Sample[effectsSampleRateHz]
		if baseSample.Len() == 0 {
			return codec.PCMFloat64{}
		}

		sample = baseSample.Resample(sampleRate)
		// Update the cache by modifying the original map entry
		s.samples[name].Sample[sampleRate] = sample
	}

	// Check if sample is empty after potential resampling
	if sample.Len() == 0 {
		return codec.PCMFloat64{}
	}

	// The cached sample is returned directly: the mixer write path
	// (ChannelMixer.WriteChannel -> MixerChannel.WriteScaled) no longer scales
	// its input in place, and the remaining read-only consumers (DCA encoding)
	// do not mutate it, so no defensive copy is required.
	return sample
}

// SetGearShiftPulse re-renders the gear shift effect at a new frequency and length,
// which is how a vehicle's gearbox character is applied.
//
// It replaces the whole per-rate map rather than just the base entry, because the
// lazily resampled copies GetSample caches are renderings of the *previous* waveform
// and would otherwise outlive it — the synthesizer runs at 8 kHz by default, so the
// cached resample is the one actually played.
//
// Callers are on the app main loop, never the audio callback path, and a sample
// already handed out stays valid: WriteScaled copies it into the ring buffer
// synchronously before returning.
func (s *EffectsSampleBank) SetGearShiftPulse(pulseHz float64, lengthSeconds float64) {
	sample := generateGearShiftSample(pulseHz, lengthSeconds)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.samples[GearShiftEffectName] = EffectSample{
		Name: GearShiftEffectName,
		Sample: map[int]codec.PCMFloat64{
			effectsSampleRateHz: sample,
		},
	}
}

// The frequency and length carry the gearbox's character, because level cannot: a
// race car's whole magnitude range plays in compression, so every race car arrives at
// the transducer within a fraction of a dB of every other one. A modern sequential
// gets a short sharp crack and a manually operated box a longer, lower, heavier thud,
// which reads as a different mechanism rather than a quieter one. See
// GearShiftPulseHz in the app package for the mapping from measured shift duration.
const (
	// gearShiftPulseAmplitude is the peak of the rendered pulse. See the note on
	// generateGearShiftSample: super-unity is load-bearing and must not be reduced.
	gearShiftPulseAmplitude = 2.0

	// gearShiftDecayEFolds is how many e-foldings the envelope decays over the length
	// of the sample, so the envelope shape is length-relative and a longer pulse is
	// simply a slower version of the same decay rather than a truncated one.
	gearShiftDecayEFolds = 5.0
)

// generateGearShiftSample renders the gear shift pulse at the given frequency and
// length.
//
// The peak deliberately exceeds unity, and reducing it is a mistake that has been
// made once already. PlayEffect scales this sample by a magnitude in
// [gain floor, 1.0] and the result is summed into the output buffer through
// softKnee, which is transparent only below softKneeThreshold (0.7), so at a peak of
// 2.0 a race car's whole magnitude range plays in compression: the knee is crossed
// at a magnitude of 0.35, which is below the race gain floor. That does cost the
// effect its dynamic range — a Porsche 963's 2.0 dB upshift-to-downshift contrast
// reaches the transducer as 0.3 dB.
//
// Dropping the peak to 0.9 to recover that range was tried and was much worse. A
// gear change has to read as a discrete event against whatever the chassis and road
// channels are already putting in the buffer, and overdriving the knee is what buys
// that: at 2.0 the pulse sets the operating point and everything else becomes the
// locally-reduced slope softCombine leaves it, so the shift dominates by
// construction. At 0.9 it merely joins the mix, and multiple Group 1 cars and a
// Super Formula were reported as having essentially no shift feedback in either
// direction — a far bigger loss than the 2.9 dB of peak level suggests, because
// prominence against the background, not peak amplitude, is what makes the event
// legible.
//
// So the compression here is load-bearing. Up/down contrast has to be found
// somewhere that does not trade away prominence.
func generateGearShiftSample(pulseHz float64, sampleLengthSeconds float64) codec.PCMFloat64 {
	pulseAmplitude := gearShiftPulseAmplitude
	decayRate := 1 - (gearShiftDecayEFolds / (float64(effectsSampleRateHz) * sampleLengthSeconds))

	sampleCount := int(sampleLengthSeconds * float64(effectsSampleRateHz))

	pulseWidth := float64(effectsSampleRateHz) / (2 * pulseHz)
	waveSamplePeriod := math.Pi / pulseWidth
	waveOffset := pulseWidth

	samples := make([]float64, sampleCount)

	for i := range samples {
		angle := waveSamplePeriod * (float64(i) - waveOffset)
		samples[i] = pulseAmplitude * math.Sin(angle)

		pulseAmplitude *= decayRate
	}

	return *codec.NewPCMFloat64(samples, effectsSampleRateHz, 1)
}

// generateTalkPermitToneSample creates a sample for the talk permit tone sequence.
func generateTalkPermitToneSample() codec.PCMFloat64 {
	toneSequence := "746839456"

	return generateDTMFSequence(toneSequence, 20*time.Millisecond, 0*time.Millisecond)
}

// generateRecordingStartToneSample creates a sample for the recording start tone sequence.
func generateRecordingStartToneSample() codec.PCMFloat64 {
	toneSequence := "79"

	return generateDTMFSequence(toneSequence, 48*time.Millisecond, 2*time.Millisecond)
}

// generateRecordingStopToneSample creates a sample for the recording stop tone sequence.
func generateRecordingStopToneSample() codec.PCMFloat64 {
	toneSequence := "97"

	return generateDTMFSequence(toneSequence, 48*time.Millisecond, 2*time.Millisecond)
}

// generateErrorToneSample creates a sample for the error tone sequence.
func generateErrorToneSample() codec.PCMFloat64 {
	toneSequence := "1111"

	return generateDTMFSequence(toneSequence, 24*time.Millisecond, 2*time.Millisecond)
}

// generateTalkPermitToneSample creates a sample for the talk permit tone sequence.
func generateDTMFSequence(toneSequence string, toneLength time.Duration, silenceLength time.Duration) codec.PCMFloat64 {
	tones := [][]float64{}

	samples := make([]float64, 0)

	// Generate tones for each character in the value
	for _, char := range toneSequence {
		tone := generateDTMFTone(string(char), toneLength)
		if len(tone) == 0 {
			// Skip empty tones but continue processing
			continue
		}

		tones = append(tones, tone)
	}

	// Append tones with silence in between
	for i, tone := range tones {
		samples = append(samples, tone...)

		// Add silence between tones
		if i < len(tones)-1 {
			silenceSampleCount := int(float64(silenceLength.Milliseconds()) * float64(effectsSampleRateHz) / 1000.0)
			silence := make([]float64, silenceSampleCount)
			samples = append(samples, silence...)
		}
	}

	return *codec.NewPCMFloat64(samples, effectsSampleRateHz, 1)
}

// generateDTMFTone generates a DTMF tone for a given character and duration.
func generateDTMFTone(value string, length time.Duration) []float64 {
	tones := map[rune][2]float64{
		'1': {697, 1209},
		'2': {697, 1336},
		'3': {697, 1477},
		'A': {697, 1633},
		'4': {770, 1209},
		'5': {770, 1336},
		'6': {770, 1477},
		'B': {770, 1633},
		'7': {852, 1209},
		'8': {852, 1336},
		'9': {852, 1477},
		'C': {852, 1633},
		'*': {941, 1209},
		'0': {941, 1336},
		'#': {941, 1477},
		'D': {941, 1633},
	}

	sampleCount := int(length.Seconds() * float64(effectsSampleRateHz))
	audioSample := make([]float64, sampleCount)

	if len(value) != 1 {
		return audioSample
	}

	frequencies, ok := tones[rune(value[0])]
	if !ok {
		return audioSample
	}

	fadeLength := int(float64(sampleCount) * 0.15)

	for index := range audioSample {
		phase := float64(index) / float64(effectsSampleRateHz)

		// Generate the base DTMF tone
		sample := 0.25 * (math.Sin(2*math.Pi*frequencies[0]*phase) + math.Sin(2*math.Pi*frequencies[1]*phase))

		// Apply fade in/out envelope
		envelope := float64(1.0)

		if index < fadeLength {
			envelope = float64(index) / float64(fadeLength)
		} else if index >= sampleCount-fadeLength {
			envelope = float64(sampleCount-index-1) / float64(fadeLength)
		}

		audioSample[index] = sample * envelope
	}

	return audioSample
}
