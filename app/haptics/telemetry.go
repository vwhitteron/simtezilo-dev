package haptics

import (
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
)

// TelemetrySource is the subset of the telemetry transformer the haptic generators
// read, extracted so a generator can be driven by a fake in a test. It is deliberately
// narrow: the generators take everything else from kinematics, config and the vehicle
// characteristics.
//
// *gttelemetry.Transformer satisfies this interface as it stands, so the live app and
// the capture harness both pass the real transformer with no adapter.
//
// Transmission returns the library's own struct rather than a plain slice of ratios,
// because the generator must not use Transformer.CurrentGearRatio: that method panics
// on reverse and returns a negative sentinel for neutral. See currentGearRatio.
type TelemetrySource interface {
	EngineRPM() float32
	ThrottleOutputPercent() float32
	Transmission() gttelemetry.Transmission
}

// TelemetryFunc supplies the current telemetry source on demand.
//
// The generators take a getter rather than a source, because the app builds its
// telemetry client after it builds the generators, and both offline harnesses replace
// the client afterwards. A captured source would be nil in the first case and stale in
// the second. This mirrors how the app hands a telemetry getter to the wind simulator.
type TelemetryFunc func() TelemetrySource
