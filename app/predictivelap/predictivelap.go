// Package predictivelap computes a live predictive lap-time delta: how far ahead
// or behind the current lap is against the fastest lap seen so far, measured at the
// same point on the track.
//
// The fastest lap's elapsed time is recorded into a fixed set of buckets keyed by
// lap progress (0..1). The current lap's elapsed time is then compared against the
// interpolated reference time at the same progress to produce the delta.
package predictivelap

import (
	"math"
	"sync"
	"time"
)

// buckets is the number of lap-progress samples retained for the reference lap.
// Sample i corresponds to progress fraction i/buckets, so there are buckets+1
// entries spanning [0, 1] inclusive.
const buckets = 200

// PredictiveLap records reference-lap split times and computes the live delta.
// It is safe for concurrent use: Record and Delta run on the main tick loop while
// CompleteLap runs on the lap-event goroutine.
type PredictiveLap struct {
	mu sync.Mutex

	ref     [buckets + 1]time.Duration // fastest lap's elapsed time at each progress sample
	refSet  [buckets + 1]bool          // which ref samples have been populated
	hasRef  bool                       // a complete reference lap has been promoted
	bestLap time.Duration              // lap time of the current reference lap

	scratch    [buckets + 1]time.Duration // in-progress lap's split times
	scratchSet [buckets + 1]bool
}

// New creates a PredictiveLap with no reference lap yet.
func New() *PredictiveLap {
	return &PredictiveLap{}
}

// Reset clears the reference lap and any in-progress samples.
func (p *PredictiveLap) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ref = [buckets + 1]time.Duration{}
	p.refSet = [buckets + 1]bool{}
	p.hasRef = false
	p.bestLap = 0
	p.scratch = [buckets + 1]time.Duration{}
	p.scratchSet = [buckets + 1]bool{}
}

// Record stores the current lap's elapsed time at the given progress (0..1). Only
// the first sample seen in each bucket is kept, approximating the elapsed time on
// first reaching that point. Out-of-range or non-finite progress is ignored.
func (p *PredictiveLap) Record(progress float64, currentLaptime time.Duration) {
	idx, ok := bucketIndex(progress)
	if !ok {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.scratchSet[idx] {
		return
	}

	p.scratch[idx] = currentLaptime
	p.scratchSet[idx] = true
}

// CompleteLap finalises the lap just completed. If it is the fastest lap so far
// (or the first complete lap), its splits become the new reference. The scratch
// buffer is always cleared ready for the next lap. A non-positive lapTime is
// treated as invalid and only clears the scratch buffer.
func (p *PredictiveLap) CompleteLap(lapTime time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if lapTime > 0 && (!p.hasRef || lapTime < p.bestLap) {
		p.ref = p.scratch
		p.refSet = p.scratchSet
		p.hasRef = true
		p.bestLap = lapTime
	}

	p.scratch = [buckets + 1]time.Duration{}
	p.scratchSet = [buckets + 1]bool{}
}

// Delta returns the predictive delta in seconds at the given progress: the
// reference lap's elapsed time minus the current lap's elapsed time. A positive
// value means the current lap is ahead (faster). ok is false until a reference lap
// exists or when no reference sample is available near progress.
func (p *PredictiveLap) Delta(progress float64, currentLaptime time.Duration) (secs float64, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.hasRef {
		return 0, false
	}

	refTime, ok := p.referenceAt(progress)
	if !ok {
		return 0, false
	}

	return (refTime - currentLaptime).Seconds(), true
}

// referenceAt returns the reference lap's elapsed time at the given progress,
// linearly interpolating between the nearest populated samples on each side.
func (p *PredictiveLap) referenceAt(progress float64) (time.Duration, bool) {
	if math.IsNaN(progress) || math.IsInf(progress, 0) {
		return 0, false
	}

	pos := clamp01(progress) * buckets

	low, high := p.bracket(int(math.Floor(pos)), int(math.Ceil(pos)))

	switch {
	case low < 0 && high > buckets:
		return 0, false
	case low < 0:
		return p.ref[high], true
	case high > buckets:
		return p.ref[low], true
	case low == high:
		return p.ref[low], true
	}

	// Interpolate between the two bracketing samples.
	frac := (pos - float64(low)) / float64(high-low)
	span := float64(p.ref[high] - p.ref[low])

	return p.ref[low] + time.Duration(frac*span), true
}

// bracket walks outwards from lo/hi to the nearest populated reference samples on
// each side, so a value can be interpolated even where samples are sparse.
func (p *PredictiveLap) bracket(low, high int) (int, int) {
	for low >= 0 && !p.refSet[low] {
		low--
	}

	for high <= buckets && !p.refSet[high] {
		high++
	}

	return low, high
}

// BucketCount is the number of lap-progress buckets spanning a lap. Display code
// can use Bucket to refresh a derived value only when progress crosses into a new
// bucket, giving roughly BucketCount updates per lap.
const BucketCount = buckets

// Bucket returns the lap-progress bucket index for a progress fraction and whether
// the progress is usable (finite and within [0, 1]). It exposes the same mapping
// Record/Delta use, so display throttling aligns with the reference resolution.
func Bucket(progress float64) (int, bool) {
	return bucketIndex(progress)
}

// bucketIndex maps a progress fraction to a sample index, reporting whether the
// fraction is usable (finite and within [0, 1]).
func bucketIndex(progress float64) (int, bool) {
	if math.IsNaN(progress) || math.IsInf(progress, 0) || progress < 0 || progress > 1 {
		return 0, false
	}

	idx := int(math.Round(progress * buckets))
	if idx < 0 {
		idx = 0
	} else if idx > buckets {
		idx = buckets
	}

	return idx, true
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}

	if value > 1 {
		return 1
	}

	return value
}
