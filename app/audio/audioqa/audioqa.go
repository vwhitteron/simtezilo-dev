// Package audioqa measures how far a captured audio signal departs from clean
// output. It reports the hard failures (clipping, NaN/Inf, underrun dropouts) and
// the soft ones (sample-to-sample discontinuities) over the active region of a
// single channel, and — for a known pure tone — the fundamental-fit gain and
// residual SNR.
//
// The metrics are tone-independent except for the fundamental fit, so the same
// code serves both the audio_cleanup diagnostic tool (which drives a known sine)
// and the haptic generation tests (whose engine waveform has no single spectral
// line, and which rely only on the discontinuity/dropout measurements).
package audioqa

import "math"

// Metrics holds tone-independent signal-quality measurements over the active
// (non-silent) region of a single channel. A clean signal has zero NonFinite,
// Clipped, Dropouts and Glitches.
type Metrics struct {
	Frames     int     // total samples supplied
	RegionFrom int     // first sample of the analysed region
	RegionTo   int     // one past the last sample of the analysed region
	Peak       float64 // largest |sample| in the region
	Clipped    int     // samples with |v| >= 0.999
	NonFinite  int     // NaN or Inf samples
	Dropouts   int     // interior zero-runs longer than two samples (underrun signature)
	MaxDropout int     // longest zero-run in samples
	MaxStep    float64 // largest |x[i]-x[i-1]| in the region
	StepBound  float64 // step considered clean; steps beyond 3x are counted as glitches
	Glitches   int     // steps exceeding 3*StepBound (0 when StepBound <= 0)
	Empty      bool    // no usable region was found
}

// Tone holds the single-frequency fit of a region against a known pure tone:
// whatever does not fit the expected sinusoid is the distortion plus noise the
// pipeline added.
type Tone struct {
	Gain      float64 // recovered fundamental amplitude / input amplitude
	RMSResid  float64 // RMS of (signal - fitted fundamental)
	PeakResid float64 // peak of the residual
	SNR       float64 // 20*log10(fundamental / (sqrt2 * RMSResid))
}

// Analyse computes tone-independent Metrics for a single channel sampled at rate
// Hz. refAmp is the approximate signal peak, used only to trim leading and
// trailing silence (pass <= 0 to analyse the whole slice). When rate > 0 a ~10 ms
// guard band is trimmed inside each edge so onset/offset ramps do not inflate the
// step and peak measurements. stepBound seeds glitch detection: steps beyond
// 3*stepBound are counted as glitches (pass <= 0 to report MaxStep without
// flagging glitches).
func Analyse(samples []float64, rate int, refAmp, stepBound float64) Metrics {
	result := Metrics{Frames: len(samples), StepBound: stepBound}

	from, end := SignalRegion(samples, refAmp)

	// Trim a short guard band (~10 ms) inside each edge so the onset/offset ramps
	// — where the signal fades up from silence — do not inflate the peak or step
	// measurements. Those ramps are real but transient; the steady-state body is
	// what reveals whether the pipeline distorts the signal.
	if rate > 0 {
		guard := rate / 100
		if from+guard < end-guard {
			from += guard
			end -= guard
		}
	}

	result.RegionFrom, result.RegionTo = from, end

	if end-from < 2 {
		result.Empty = true

		return result
	}

	scanRegion(samples[from:end], stepBound, &result)
	result.Dropouts, result.MaxDropout = ZeroRuns(samples[from:end])

	return result
}

// AnalyseTone analyses samples against a known pure tone of frequency freq and
// input amplitude inAmp, returning the tone-independent Metrics plus the
// fundamental fit. The region must span at least one cycle or Metrics.Empty is
// set.
func AnalyseTone(samples []float64, rate int, freq, inAmp float64) (Metrics, Tone) {
	stepBound := inAmp * 2 * math.Pi * freq / float64(rate)

	result := Analyse(samples, rate, inAmp, stepBound)
	if result.Empty {
		return result, Tone{}
	}

	// A fundamental fit needs at least one full cycle to be meaningful.
	if result.RegionTo-result.RegionFrom < rate/int(math.Max(freq, 1)) {
		result.Empty = true

		return result, Tone{}
	}

	return result, fitFundamental(samples[result.RegionFrom:result.RegionTo], rate, freq, inAmp)
}

// scanRegion fills the per-sample hard checks and discontinuity measurements for
// region into result.
func scanRegion(region []float64, stepBound float64, result *Metrics) {
	for index, value := range region {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			result.NonFinite++

			continue
		}

		if magnitude := math.Abs(value); magnitude > result.Peak {
			result.Peak = magnitude
		}

		if math.Abs(value) >= 0.999 {
			result.Clipped++
		}

		if index == 0 {
			continue
		}

		step := math.Abs(value - region[index-1])
		if step > result.MaxStep {
			result.MaxStep = step
		}

		if stepBound > 0 && step > 3*stepBound {
			result.Glitches++
		}
	}
}

// fitFundamental projects region onto cos/sin at freq and returns the recovered
// amplitude and the residual after subtracting the best-fit sinusoid. The phase
// is absorbed by the two coefficients, so no alignment is needed.
func fitFundamental(region []float64, rate int, freq, inAmp float64) Tone {
	omega := 2 * math.Pi * freq / float64(rate)
	count := float64(len(region))

	var cosSum, sinSum float64

	for index, value := range region {
		cosSum += value * math.Cos(omega*float64(index))
		sinSum += value * math.Sin(omega*float64(index))
	}

	cosSum *= 2 / count
	sinSum *= 2 / count
	fundAmp := math.Hypot(cosSum, sinSum)

	var tone Tone

	tone.Gain = fundAmp / inAmp

	var sumSq float64

	for index, value := range region {
		fit := cosSum*math.Cos(omega*float64(index)) + sinSum*math.Sin(omega*float64(index))
		resid := value - fit

		sumSq += resid * resid
		if magnitude := math.Abs(resid); magnitude > tone.PeakResid {
			tone.PeakResid = magnitude
		}
	}

	tone.RMSResid = math.Sqrt(sumSq / count)
	if tone.RMSResid > 0 {
		tone.SNR = 20 * math.Log10(fundAmp/(math.Sqrt2*tone.RMSResid))
	} else {
		tone.SNR = math.Inf(1)
	}

	return tone
}

// SignalRegion returns the [from,end) range carrying signal, trimming leading and
// trailing silence by locating the first and last samples that reach a quarter of
// refAmp. A non-positive refAmp returns the whole slice.
func SignalRegion(samples []float64, refAmp float64) (from, end int) {
	if refAmp <= 0 {
		return 0, len(samples)
	}

	thresh := 0.25 * refAmp

	from = 0
	for from < len(samples) && math.Abs(samples[from]) < thresh {
		from++
	}

	end = len(samples)
	for end > from && math.Abs(samples[end-1]) < thresh {
		end--
	}

	return from, end
}

// ZeroRuns counts interior runs of exact zeros longer than two samples — the
// signature of a ring-buffer underrun, which zero-pads the device callback.
func ZeroRuns(samples []float64) (count, longest int) {
	run := 0

	flush := func() {
		if run > 2 {
			count++

			if run > longest {
				longest = run
			}
		}

		run = 0
	}

	for _, value := range samples {
		if value == 0 {
			run++
		} else {
			flush()
		}
	}

	flush()

	return count, longest
}
