package internal

import (
	"fmt"
	"math"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/gopxl/beep/vorbis"
	"github.com/rs/zerolog"
)

type AudioOut struct {
	samples map[string]*beep.Buffer
	log     zerolog.Logger
}

type AudioOutOpts struct {
	AssetDir string
	Logger   zerolog.Logger
}

func NewAudioOutputDevice(opts AudioOutOpts) (*AudioOut, error) {
	audioSamples := []string{
		"gearchange",
	}

	buffers := map[string]*beep.Buffer{}

	for _, sampleName := range audioSamples {
		sampleFile := opts.AssetDir + "/audio/" + sampleName + ".ogg"
		sampleData, err := os.Open(sampleFile)
		if err != nil {
			return nil, fmt.Errorf("reading file %q: %w", sampleFile, err)
		}

		streamer, format, err := vorbis.Decode(sampleData)
		if err != nil {
			return nil, fmt.Errorf("decoding %s.ogg: %w", sampleName, err)
		}

		bufferSize := format.SampleRate.N(time.Second / 15)
		speaker.Init(format.SampleRate, bufferSize)

		opts.Logger.Debug().
			Int("sample rate", int(format.SampleRate)).
			Int("buffer size", bufferSize).
			Msgf("loaded audio stream %q", sampleName)

		buffers[sampleName] = beep.NewBuffer(format)
		buffers[sampleName].Append(streamer)
		streamer.Close()
	}

	return &AudioOut{
		samples: buffers,
		log:     opts.Logger,
	}, nil
}

func (a *AudioOut) Play(name string, gain float64) {
	if _, ok := a.samples[name]; !ok {
		a.log.Info().Msgf("unable to play unknown audio stream %q", name)
		return
	}

	if gain >= maxGain {
		gain = maxGain
	}

	streamer := a.samples[name].Streamer(0, a.samples[name].Len())

	sample := &effects.Volume{
		Streamer: streamer,
		Base:     10,
		Volume:   gain / 10,
	}

	speaker.Play(sample)
}

type BumpStream struct {
	audioBuffer *[]float64
	physics     *physicsTracker
	gain        *float64
	enabled     bool
}

func NewBumpStream(buffer *[]float64, physics *physicsTracker, gain *float64, enabled bool) BumpStream {
	return BumpStream{
		audioBuffer: buffer,
		physics:     physics,
		gain:        gain,
		enabled:     enabled,
	}
}

func (b BumpStream) Stream(samples [][2]float64) (n int, ok bool) {
	buffer := *b.audioBuffer

	for i := range samples {
		sample := buffer[i] * *b.gain
		samples[i][0] = sample
		samples[i][1] = sample
	}

	return len(samples), true
}

func (b BumpStream) Stream1(samples [][2]float64) (n int, ok bool) {
	// startTime := time.Now()

	// thisAmplitudeL := compressDR(b.physics.current.jerk)
	thisAmplitudeR := compressDR2(b.physics.current.jerk)
	snap := compressDR1(b.physics.current.snap)
	impact := thisAmplitudeR * snap
	periodReduction := 0.0
	if impact < 6 {
		periodReduction = snap * 2
	}

	sampleLen := len(samples)

	// if !b.enabled || b.physics.current.sequenceID == b.physics.last.sequenceID {
	if !b.enabled {
		for i := range samples {
			samples[i][0] = 0
			samples[i][1] = 0
		}

		return sampleLen, true
	}

	// linear interpolation
	// sampleIncr := (currentAmplitude - lastAmplitude) / float64(sampleLen)
	// linearValue := lastAmplitude
	// for i := range samples {
	// 	linearValue += sampleIncr
	// 	samples[i][0] = linearValue * *b.gain
	// 	samples[i][1] = linearValue * *b.gain
	// }

	// waveLenL := float64(sampleLen)
	// periodLenL := waveLenL / 2
	// offsetL := periodLenL / 2
	// samplePeriodL := math.Pi / periodLenL

	periodLenR := float64(sampleLen) / 2

	if periodReduction > 0 {
		periodLenR = periodLenR - periodReduction
	} else if periodReduction < 0 {
		periodLenR = periodLenR + periodReduction
	}

	// Limit upper frequency to 26 (153.46hz) min frequency is 133 (30 Hz)
	// if periodLenR < 26 {
	// 	periodLenR = 26
	// }

	waveLenR := periodLenR * 2
	offsetR := periodLenR / 2
	samplePeriodR := math.Pi / periodLenR
	// samplePeriod := math.Pi / (float64(sampleLen) / 2)

	// peakL := 0.0
	peakR := 0.0
	for i := range samples {
		// if float64(i) > waveLenL {
		// 	samples[i][0] = 0
		// } else {
		// 	sineValueL := thisAmplitudeL*math.Sin(samplePeriodL*(float64(i)-offsetL)) + thisAmplitudeL
		// 	sampleValueL := sineValueL * *b.gain
		// 	samples[i][0] = sampleValueL

		// 	if sampleValueL > 0 && sampleValueL > peakL {
		// 		peakL = sineValueL
		// 	} else if sampleValueL < 0 && sampleValueL < peakL {
		// 		peakL = sineValueL
		// 	}
		// }

		if float64(i) > waveLenR {
			samples[i][0] = 0
			samples[i][1] = 0
		} else {
			sineValueR := thisAmplitudeR*math.Sin(samplePeriodR*(float64(i)-offsetR)) + thisAmplitudeR
			sampleValueR := sineValueR * *b.gain
			samples[i][0] = sampleValueR
			samples[i][1] = sampleValueR

			if sampleValueR > 0 && sampleValueR > peakR {
				peakR = sineValueR
			} else if sampleValueR < 0 && sampleValueR < peakR {
				peakR = sineValueR
			}
		}
	}

	// if impact > 4 {
	// thing := b.physics.current.jerk * b.physics.current.snap
	// duration := time.Since(startTime)
	// fmt.Printf("INPUT: jerk: %0.05f, snap: %0.05f, thing: %0.05f, impact: %0.05f, gain: %0.05f, time: %v seq: %d\n", b.physics.current.jerk, b.physics.current.snap, thing, impact, *b.gain, duration, b.physics.current.sequenceID)
	// fmt.Printf("LEFT:  peak: %0.05f, amplitude: %0.05f, reduce: 0.0000, samplePeriod: %0.05f, periodLen: %0.05f, waveLen: %0.05f\n", peakL, thisAmplitudeL, samplePeriodL, periodLenL, waveLenL)
	// fmt.Printf("RIGHT: peak: %0.05f, amplitude: %0.05f, reduce: %0.05f, samplePeriod: %0.05f, periodLen: %0.05f, waveLen: %0.05f\n\n", peakR, thisAmplitudeR, periodReduction, samplePeriodR, periodLenR, waveLenR)
	// }

	return sampleLen, true
}

func (b BumpStream) Err() error {
	return nil
}

func compressDR1(source float64) float64 {
	power := 0.5
	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	// reduce large signals
	compressed := math.Pow(source, power)

	// don't amplify small signals
	if compressed > source {
		compressed = source
	}

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

func compressDR2(source float64) float64 {
	power := 0.4

	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	// reduce large signals
	compressed := math.Pow(source, power)

	// don't amplify small signals
	if compressed > source {
		compressed = source
	}

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

func functionLog10(source float64) float64 {
	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	compressed := math.Log10(source + 1)

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

func functionExponent(source float64, exponent float64) float64 {
	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	result := math.Pow(source, exponent)

	if isNeg {
		result = -result
	}

	return result
}

func functionLog2(source float64) float64 {
	isNeg := false

	if source < 0 {
		isNeg = true
		source = -source
	}

	compressed := math.Log2(source + 1)

	if isNeg {
		compressed = -compressed
	}

	return compressed
}

func functionScale(source float64, scale float64) float64 {

	return source * scale
}
