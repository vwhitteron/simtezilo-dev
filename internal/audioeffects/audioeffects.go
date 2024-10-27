package audioeffects

import (
	"math"
)

type Samples struct {
	sample map[string][]float64
}

func NewAudioEffects(sampleRateHz int) *Samples {
	return &Samples{
		sample: map[string][]float64{
			"gearchange": generateGearChangeSample(sampleRateHz),
		},
	}
}

func (s *Samples) GetSample(name string) []float64 {
	if _, ok := s.sample[name]; !ok {
		return []float64{}
	}

	return s.sample[name]
}

func generateGearChangeSample(sampleRateHz int) []float64 {
	sampleLengthSeconds := 0.1
	pulseAmplitude := 1.6
	pulseHz := 30
	decayRate := 0.005

	sampleCount := int(sampleLengthSeconds * float64(sampleRateHz))

	pulseWidth := sampleRateHz / (2 * pulseHz)
	waveSamplePeriod := math.Pi / float64(pulseWidth)
	waveOffset := float64(pulseWidth)

	audioSample := make([]float64, sampleCount)
	// audioSampleInt := make([]int, sampleCount)

	for i := range audioSample {
		angle := waveSamplePeriod * (float64(i) - waveOffset)
		audioSample[i] = (pulseAmplitude * math.Sin(angle)) / 2

		// audioSampleInt[i] = int(audioSample[i] * 32767)

		pulseAmplitude = pulseAmplitude * (1 - decayRate)
	}

	// fmt.Printf("\n\nSample: %+v\n\n", audioSample)
	// fmt.Printf("\n\nSample: %+v\n\n", audioSampleInt)

	// out, err := os.Create("gearchange.wav")
	// if err != nil {
	// 	panic(fmt.Sprintf("couldn't create output file - %v", err))
	// }

	// bitDepth := 16
	// channels := 1
	// format := 1

	// buf := &goaudio.IntBuffer{
	// 	Format: &goaudio.Format{
	// 		NumChannels: channels,
	// 		SampleRate:  sampleRateHz,
	// 	},
	// 	SourceBitDepth: bitDepth,
	// 	Data:           audioSampleInt,
	// }

	// e := wav.NewEncoder(out,
	// 	sampleRateHz,
	// 	bitDepth,
	// 	channels,
	// 	format)
	// if err = e.Write(buf); err != nil {
	// 	panic(err)
	// }
	// // close the encoder to make sure the headers are properly
	// // set and the data is flushed.
	// if err = e.Close(); err != nil {
	// 	panic(err)
	// }
	// out.Close()

	return audioSample
}
