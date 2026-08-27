// engine_profile.go derives a vehicle's engine haptic characteristics: firing
// frequency, pulse overlap and the profile lookup. It was extracted from package app
// so the CGO-free capture, and through it the tuning assistant, can render the engine
// layer. Without this derivation EngineGenerator has no firing frequency and renders
// silence.
//
// The package godoc lives in generator.go.

package haptics

import (
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics/profiles"
	"github.com/vwhitteron/simtezilo-dev/app/vehicle"
	gttelemetry "github.com/zetetos/gt-telemetry/v2"
)

// defaultRevLimit stands in when telemetry reports no rev limit, so the rpm fraction
// that scales the engine haptic never divides by zero.
const defaultRevLimit uint16 = 8000

// EngineForVehicle derives the current vehicle's engine haptic characteristics from
// telemetry and clamps the pulse rate to what the transducer can follow. It returns the
// characteristics and the normalised rev limit.
//
// This is the single entry point the app, the app-side capture harness and the CGO-free
// capture all use, so the three cannot drift apart. A vehicle with no engine layout
// yields zero-valued characteristics and a logged error: the engine layer then renders
// silence, which is the same outcome the app had before.
func EngineForVehicle(
	cfg *config.Config, client *gttelemetry.Client, logger zerolog.Logger,
) (vehicle.EngineCharacteristics, uint16) {
	layout := client.Telemetry.VehicleEngineLayout()
	bankAngle := client.Telemetry.VehicleEngineBankAngle()
	crankPlaneAngle := client.Telemetry.VehicleEngineCrankPlaneAngle()
	revLimit := client.Telemetry.EngineRPMLight().Max

	engine, err := engineCharacteristics(cfg, layout, bankAngle, crankPlaneAngle, revLimit)
	if err != nil {
		logger.Error().
			Err(err).
			Str("engine_layout", layout).
			Float32("cylinder_angle", bankAngle).
			Float32("crank_plane_angle", crankPlaneAngle).
			Msg("failed to get engine characteristics")
	}

	clampPulseRate(&engine, revLimit)

	return engine, NormalizeRevLimit(revLimit)
}

// ResolveEngineProfile reports the engine profile key the vehicle on client resolves
// to, together with the profile itself. It builds its own default config, the same
// way CaptureChassis does, so a caller with no config of its own — the tuning
// assistant's analysis pass — can show which profile a replay will be rendered with.
//
// A vehicle that matches no entry yields an empty key and the neutral profile, which
// is what the engine layer renders with.
func ResolveEngineProfile(client *gttelemetry.Client) (string, profiles.EngineProfile) {
	cfg := config.New(config.Options{Logger: zerolog.New(io.Discard)})

	engine, _ := EngineForVehicle(cfg, client, zerolog.New(io.Discard))

	if engine.Haptics == nil {
		return "", profiles.EngineProfile{}
	}

	// Lowercased to match how the config stores and looks up its keys, so the name
	// shown to a user is the one they will find in the config file.
	return strings.ToLower(engine.DBEntry), *engine.Haptics
}

// NormalizeRevLimit substitutes a default when telemetry reports no rev limit.
func NormalizeRevLimit(revLimit uint16) uint16 {
	if revLimit == 0 {
		return defaultRevLimit
	}

	return revLimit
}

// clampPulseRate scales the pulse rate down when the engine's natural firing rate at
// the rev limit would exceed what the transducer can follow.
func clampPulseRate(engine *vehicle.EngineCharacteristics, revLimit uint16) {
	if engine.Haptics == nil {
		return
	}

	peakNaturalPulseRate := float64(revLimit) * engine.FiringFrequency
	peakPulseRate := peakNaturalPulseRate * engine.Haptics.PulseScale

	if peakPulseRate > maxPulseRate {
		engine.Haptics.PulseScale = (maxPulseRate / peakPulseRate) * engine.Haptics.PulseScale
	}
}

const maxPulseRate float64 = 300.0 // Max pulse rate for engine haptics

// engineCharacteristics retrieves engine characteristics based on a given engine geometry and speed.
func engineCharacteristics(
	cfg *config.Config,
	engineLayout string,
	cylinderAngle float32,
	crankPlaneAngle float32,
	revLimit uint16,
) (vehicle.EngineCharacteristics, error) {
	if engineLayout == "" {
		return vehicle.EngineCharacteristics{
			Haptics: &profiles.EngineProfile{},
		}, errors.New("engine layout not provided")
	}

	geometryCode := engineLayout[:1] // Get first character for geometry type

	chambers, err := strconv.Atoi(engineLayout[1:]) // Get remaining characters for chamber count
	if err != nil {
		return vehicle.EngineCharacteristics{}, err // Return error if conversion fails
	}

	characteristics := vehicle.EngineCharacteristics{
		Layout:          engineLayout,
		DBEntry:         "",
		Geometry:        geometryCode,
		Chambers:        chambers,
		RevLimit:        revLimit,
		FiringFrequency: getEngineFiringFrequency(geometryCode, chambers),
		PulseOverlap:    0.5 - calculatePulseOverlap(cylinderAngle, crankPlaneAngle, chambers, geometryCode),
		Haptics: &profiles.EngineProfile{
			PrimaryBalance:   1.0,
			SecondaryBalance: 1.0,
			Gain:             config.MinimumGain,
			PulseScale:       1.0,
		},
	}

	revRange := "std"

	switch {
	case revLimit > 13000:
		revRange = "high"
	case revLimit > 9000:
		revRange = "med"
	case revLimit < 6000:
		revRange = "low"
	}

	cylinderAngleStr := strconv.FormatFloat(float64(cylinderAngle), 'f', 0, 32)
	crankPlaneAngleStr := strconv.FormatFloat(float64(crankPlaneAngle), 'f', 0, 32)

	layoutVariations := []string{
		engineLayout + "_b" + cylinderAngleStr + "_c" + crankPlaneAngleStr + "_r" + revRange,
		engineLayout + "_b" + cylinderAngleStr + "_c" + crankPlaneAngleStr,
		engineLayout + "_c" + crankPlaneAngleStr + "_r" + revRange,
		engineLayout + "_c" + crankPlaneAngleStr,
		engineLayout + "_b" + cylinderAngleStr + "_r" + revRange,
		engineLayout + "_b" + cylinderAngleStr,
		engineLayout + "_r" + revRange,
		engineLayout,
	}

	for _, variation := range layoutVariations {
		profile := cfg.GetHapticsEngineProfile(variation)
		if profile == nil {
			continue
		}

		characteristics.DBEntry = variation
		characteristics.Haptics = profile

		break
	}

	return characteristics, nil
}

// getEngineFiringFrequency calculates the firing frequency based on engine geometry and chamber count.
func getEngineFiringFrequency(geometry string, chambers int) float64 {
	switch geometry {
	case "":
		return 0.0 // No engine haptics
	case "K": // Wankel rotary engines fire 3 times per rotor per revolution
		return (float64(chambers) * 3.0) / 60.0
	case "S": // Two stroke engines fire once per cylinder every revolution
		return float64(chambers) / 60.0
	default: // Four stroke engines fire once per cylinder every 2 revolutions
		return (float64(chambers) / 2.0) / 60.0
	}
}

// calculatePulseOverlap calculates pulse overlap based on alignment between crank plane angle and cylinder bank angle.
// Returns overlap factor from 0.0 (no overlap/perfect alignment) to 1.0 (maximum overlap/misalignment).
// This value is currently used for pulse width overlap. Timing clustering is disabled pending better implementation.
// - Low values (0.0-0.2): Well-aligned engines (e.g., 60° V6 with 120° crank).
// - High values (0.3-0.8): Misaligned engines (e.g., 90° V6 with 120° crank).
func calculatePulseOverlap(cylinderAngle, crankPlaneAngle float32, chambers int, geometry string) float64 {
	// Handle special cases first
	if overlap := getSpecialCaseOverlap(geometry, chambers); overlap >= 0 {
		return overlap
	}

	// Calculate firing offset and normalize it
	firingOffset := normalizeFiringOffset(cylinderAngle, crankPlaneAngle)

	// Calculate base overlap and alignment factor based on geometry
	baseOverlap, alignmentFactor := calculateGeometryBasedOverlap(geometry, chambers, firingOffset)

	// Apply chamber count scaling
	chamberScale := calculateChamberScale(chambers, geometry)

	// Calculate final overlap and clamp to valid range
	finalOverlap := baseOverlap * alignmentFactor * chamberScale

	return clampOverlap(finalOverlap)
}

// getSpecialCaseOverlap handles special cases that don't require complex calculations.
// Returns -1 if not a special case, otherwise returns the overlap value.
func getSpecialCaseOverlap(geometry string, chambers int) float64 {
	// Wankel overlap based on rotor count and housing design
	if geometry == "K" {
		if chambers > 1 {
			return 0.15 + (float64(chambers-1) * 0.05) // 15-25% overlap for multi-rotor
		}

		return 0.05 // Single rotor has minimal overlap
	}

	// Single cylinder engines have no overlap
	if chambers <= 1 {
		return 0.0
	}

	return -1
}

// normalizeFiringOffset calculates and normalizes the firing offset angle.
func normalizeFiringOffset(cylinderAngle, crankPlaneAngle float32) float64 {
	firingOffset := math.Abs(float64(cylinderAngle - crankPlaneAngle))

	// Normalize to 0-180 degree range using modulus (angles are symmetric)
	return math.Mod(firingOffset, 180.0)
}

// calculateGeometryBasedOverlap calculates base overlap and alignment factor based on engine geometry.
func calculateGeometryBasedOverlap(geometry string, chambers int, firingOffset float64) (baseOverlap, alignmentFactor float64) {
	switch geometry {
	case "K": // Wankel engines have unique firing characteristics
		return calculateWankelOverlap(chambers)
	case "S": // 2-strokes fire every revolution, creating more natural overlap
		return calculateTwoStrokeOverlap(firingOffset)
	default: // 4-strokes fire every other revolution, creating different overlap characteristics
		return calculateFourStrokeOverlap(chambers, firingOffset)
	}
}

// calculateWankelOverlap calculates overlap for Wankel engines.
func calculateWankelOverlap(chambers int) (baseOverlap, alignmentFactor float64) {
	baseOverlap = 0.2 // Lower base overlap due to rotor design

	// Cylinder count affects overlap potential
	cylinderFactor := math.Min(float64(chambers)/4.0, 1.0)
	baseOverlap *= (0.5 + cylinderFactor*0.5)
	alignmentFactor = 1.0 // Wankels don't use alignment factor in current logic

	return baseOverlap, alignmentFactor
}

// calculateTwoStrokeOverlap calculates overlap for 2-stroke engines.
func calculateTwoStrokeOverlap(firingOffset float64) (baseOverlap, alignmentFactor float64) {
	baseOverlap = 0.3 // Higher base overlap due to rapid firing

	alignmentFactor = getTwoStrokeAlignmentFactor(firingOffset)

	return baseOverlap, alignmentFactor
}

// getTwoStrokeAlignmentFactor calculates alignment factor for 2-stroke engines based on firing offset.
func getTwoStrokeAlignmentFactor(firingOffset float64) float64 {
	switch {
	case firingOffset <= 15.0:
		// Near-perfect alignment: minimal overlap due to synchronized firing
		return 0.3
	case firingOffset >= 75.0 && firingOffset <= 105.0:
		// Perpendicular arrangement: maximum overlap
		return 1.0
	case firingOffset < 45.0:
		// Progressive alignment: interpolate between min and max
		return 0.3 + ((firingOffset-15.0)/30.0)*0.4 // 0.3 to 0.7
	default:
		return 0.7 + ((75.0-firingOffset)/30.0)*0.3 // 0.7 to 1.0
	}
}

// calculateFourStrokeOverlap calculates overlap for 4-stroke engines.
func calculateFourStrokeOverlap(chambers int, firingOffset float64) (baseOverlap, alignmentFactor float64) {
	baseOverlap = 0.2 // Lower base overlap due to spaced firing intervals

	// Cylinder count affects overlap potential
	cylinderFactor := math.Min(float64(chambers)/8.0, 1.0) // Normalize to 8-cylinder reference
	baseOverlap *= (0.5 + cylinderFactor*0.5)              // Scale: 50% to 100% of base

	alignmentFactor = getFourStrokeAlignmentFactor(firingOffset)

	return baseOverlap, alignmentFactor
}

// getFourStrokeAlignmentFactor calculates alignment factor for 4-stroke engines based on firing offset.
func getFourStrokeAlignmentFactor(firingOffset float64) float64 {
	switch {
	case firingOffset <= 10.0:
		// Near-perfect alignment: synchronized banks, minimal overlap
		return 0.2
	case firingOffset >= 80.0 && firingOffset <= 100.0:
		// Near-perpendicular: optimal staggered firing, maximum overlap
		return 1.0
	case firingOffset >= 170.0:
		// Near-opposite: boxer-style layout, minimal overlap
		return 0.1
	case firingOffset < 45.0:
		// Moving away from alignment toward staggered
		return 0.2 + ((firingOffset-10.0)/35.0)*0.5 // 0.2 to 0.7
	case firingOffset < 90.0:
		// Approaching optimal stagger
		return 0.7 + ((firingOffset-45.0)/35.0)*0.3 // 0.7 to 1.0
	case firingOffset < 135.0:
		// Moving past optimal toward opposite
		return 1.0 - ((firingOffset-90.0)/45.0)*0.6 // 1.0 to 0.4
	default:
		// Approaching opposite layout
		return 0.4 - ((firingOffset-135.0)/35.0)*0.3 // 0.4 to 0.1
	}
}

// calculateChamberScale applies chamber count scaling for engines with many cylinders.
func calculateChamberScale(chambers int, geometry string) float64 {
	switch {
	case chambers >= 8:
		// More cylinders = more potential for overlap
		return 1.0 + (float64(chambers-8) * 0.05) // +5% per cylinder above 8
	case chambers <= 4 && geometry != "S":
		// Fewer cylinders = less overlap potential (except 2-strokes)
		return 0.6 + (float64(chambers-2) * 0.2) // 60% for 2-cyl, 80% for 3-cyl, 100% for 4-cyl
	default:
		return 1.0
	}
}

// clampOverlap ensures overlap stays within reasonable bounds.
func clampOverlap(overlap float64) float64 {
	switch {
	case overlap > 0.8:
		return 0.8 // Maximum 80% overlap
	case overlap < 0.0:
		return 0.0 // No negative overlap
	default:
		return overlap
	}
}
