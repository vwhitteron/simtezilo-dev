package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	gttelemetry "github.com/zetetos/gt-telemetry/v2"
	gtmodels "github.com/zetetos/gt-telemetry/v2/pkg/models"
)

// lapSpan gives the sequence axis range covered by one lap.
type lapSpan struct {
	Lap  int16 `json:"lap"`
	From int   `json:"from"`
	To   int   `json:"to"`
}

// telemetryPreview holds one entry per packet, in packet order.
// Parallel arrays are used because an array of objects triples the JSON size.
type telemetryPreview struct {
	Name       string    `json:"name"`
	Packets    int       `json:"packets"`
	Span       int       `json:"span"`
	PacketSize int       `json:"packetSize"`
	Laps       []lapSpan `json:"laps"`
	Frame      []int     `json:"frame"`
	Lap        []int16   `json:"lap"`
	Speed      []float64 `json:"speed"`
	RPM        []float64 `json:"rpm"`
	Gear       []int     `json:"gear"`
	X          []float64 `json:"x"`
	Z          []float64 `json:"z"`
}

// round1 rounds to one decimal place, which keeps the payload small.
func round1(value float64) float64 {
	return math.Round(value*10) / 10
}

// round2 rounds to two decimal places.
func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

// buildPreview decodes a replay into the per-frame data the UI plots.
//
// The decoder in gt-telemetry sits behind an internal package, so a single
// packet cannot be decoded alone. Two passes over the same packet order are
// zipped instead: one for the sequence axis, one for the decoded fields.
func buildPreview(ctx context.Context, path string) (telemetryPreview, error) {
	var preview telemetryPreview

	packets, err := readReplay(path)
	if err != nil {
		return preview, err
	}

	frames, shape := sequenceFrames(packets)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return preview, fmt.Errorf("resolve replay path: %w", err)
	}

	preview = newPreview(filepath.Base(path), packets, frames, shape)

	client, err := gttelemetry.New(gttelemetry.Options{
		Source:   "file://" + absPath,
		Format:   gtmodels.Addendum3,
		LogLevel: "error",
	})
	if err != nil {
		return preview, fmt.Errorf("open replay decoder: %w", err)
	}

	err = decodeInto(ctx, client, &preview)
	if err != nil {
		return preview, err
	}

	preview.Laps = buildLapSpans(preview)

	return preview, nil
}

// newPreview allocates the parallel arrays and fills the sequence axis.
func newPreview(name string, packets []packet, frames []int, shape replayShape) telemetryPreview {
	count := len(packets)

	preview := telemetryPreview{
		Name:       name,
		Packets:    count,
		Span:       shape.span,
		PacketSize: len(packets[0].data),
		Frame:      frames,
		Lap:        make([]int16, count),
		Speed:      make([]float64, count),
		RPM:        make([]float64, count),
		Gear:       make([]int, count),
		X:          make([]float64, count),
		Z:          make([]float64, count),
	}

	return preview
}

// decodeInto walks the replay and fills the decoded fields by packet order.
func decodeInto(ctx context.Context, client *gttelemetry.Client, preview *telemetryPreview) error {
	index := 0

	for frame, frameErr := range client.Scan(ctx) {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if frameErr != nil {
			return fmt.Errorf("decode replay: %w", frameErr)
		}

		if index >= preview.Packets {
			break
		}

		// Scan reuses one transformer, so every value must be copied out.
		position := frame.PositionalMapCoordinates()

		preview.Lap[index] = frame.CurrentLap()
		preview.Speed[index] = round1(float64(frame.GroundSpeedMetresPerSecond()) * 3.6)
		preview.RPM[index] = math.Round(float64(frame.EngineRPM()))
		preview.Gear[index] = frame.CurrentGear()
		preview.X[index] = round2(float64(position.X))
		preview.Z[index] = round2(float64(position.Z))

		index++
	}

	if index != preview.Packets {
		fmt.Fprintf(os.Stderr,
			"warning: decoded %d frames but split %d packets, preview may be short\n",
			index, preview.Packets)
	}

	return nil
}

// buildLapSpans collects the sequence axis range of every lap that appears.
func buildLapSpans(preview telemetryPreview) []lapSpan {
	bounds := make(map[int16]*lapSpan)

	for index, lap := range preview.Lap {
		// Skip the lead-in, where the game reports no lap yet.
		if lap < 1 {
			continue
		}

		frame := preview.Frame[index]

		span, ok := bounds[lap]
		if !ok {
			bounds[lap] = &lapSpan{Lap: lap, From: frame, To: frame}

			continue
		}

		if frame < span.From {
			span.From = frame
		}

		if frame > span.To {
			span.To = frame
		}
	}

	spans := make([]lapSpan, 0, len(bounds))
	for _, span := range bounds {
		spans = append(spans, *span)
	}

	sort.Slice(spans, func(a int, b int) bool { return spans[a].Lap < spans[b].Lap })

	return spans
}
