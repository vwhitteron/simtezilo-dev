package app

import (
	"math"
	"testing"
	"time"
)

// TestTickerPeriodExact locks in the fix for the integer-division ticker bug:
// time.Second/rate must yield the true period, not the millisecond-truncated
// value that ran 120 Hz at 8 ms (125 Hz) and 60 Hz at 16 ms (62.5 Hz).
func TestTickerPeriodExact(t *testing.T) {
	cases := []struct {
		rate int
		want time.Duration
		// buggy is the old (1000/rate)*time.Millisecond value we must not equal.
		buggy time.Duration
	}{
		{hapticFrameRate, 8333333 * time.Nanosecond, 8 * time.Millisecond},
		{telemetryFrameRate, 16666666 * time.Nanosecond, 16 * time.Millisecond},
		{engineHapticFrameRate, 33333333 * time.Nanosecond, 33 * time.Millisecond},
	}

	for _, c := range cases {
		got := tickerPeriod(c.rate)
		if got != c.want {
			t.Errorf("tickerPeriod(%d) = %v, want %v", c.rate, got, c.want)
		}

		if got == c.buggy {
			t.Errorf("tickerPeriod(%d) = %v, must not equal the truncated %v", c.rate, got, c.buggy)
		}
	}
}

func TestBufferLatencyMs(t *testing.T) {
	cases := []struct {
		used int
		rate float64
		want float64
	}{
		{8000, 8000, 1000}, // a full second of 8 kHz mono samples
		{800, 8000, 100},
		{0, 8000, 0},
		{100, 0, 0}, // guard: zero rate
	}

	for _, c := range cases {
		got := bufferLatencyMs(c.used, c.rate)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("bufferLatencyMs(%d, %v) = %v, want %v", c.used, c.rate, got, c.want)
		}
	}
}

func TestDriftMs(t *testing.T) {
	const (
		outputRate    = 48000.0
		telemetryRate = 60.0
	)

	cases := []struct {
		name                   string
		framesRead, baseFrames int64
		seq, baseSeq           uint32
		want                   float64
	}{
		{
			name:       "in sync: one second of both clocks",
			framesRead: 48000, baseFrames: 0,
			seq: 60, baseSeq: 0,
			want: 0,
		},
		{
			name:       "audio lagging: telemetry 1s, audio 0.9s",
			framesRead: 43200, baseFrames: 0, // 0.9 s of audio
			seq: 60, baseSeq: 0, // 1.0 s of telemetry
			want: 100, // +100 ms behind real time
		},
		{
			name:       "audio leading: telemetry 1s, audio 1.1s",
			framesRead: 52800, baseFrames: 0, // 1.1 s of audio
			seq: 60, baseSeq: 0,
			want: -100,
		},
	}

	for _, c := range cases {
		got := driftMs(c.framesRead, c.baseFrames, c.seq, c.baseSeq, outputRate, telemetryRate)
		if math.Abs(got-c.want) > 1e-6 {
			t.Errorf("%s: driftMs = %v, want %v", c.name, got, c.want)
		}
	}

	if got := driftMs(48000, 0, 60, 0, 0, telemetryRate); got != 0 {
		t.Errorf("driftMs with zero outputRate = %v, want 0", got)
	}
}
