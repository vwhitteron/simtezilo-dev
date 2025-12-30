package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// ValidationError represents a single validation error with field and message.
type ValidationError struct {
	Field   string `json:"field"`   //nolint:tagliatelle // lowercase for easy compatibility
	Message string `json:"message"` //nolint:tagliatelle
}

// ValidationResult contains the result of configuration validation.
type ValidationResult struct { //nolint:errname // not applicable
	Valid  bool              `json:"valid"`            //nolint:tagliatelle // lowercase for easy compatibility
	Errors []ValidationError `json:"errors,omitempty"` //nolint:tagliatelle
}

// Error implements the error interface for ValidationResult.
func (vr ValidationResult) Error() string {
	if vr.Valid {
		return ""
	}

	var messages []string //nolint:prealloc // ubknown but small number of errors expected
	for _, err := range vr.Errors {
		messages = append(messages, fmt.Sprintf("%s: %s", err.Field, err.Message))
	}

	return strings.Join(messages, "; ")
}

// Validator defines the interface for configuration validation.
// This interface allows for easy replacement with external validators in the future.
type Validator interface {
	Validate() ValidationResult
}

// Validate performs comprehensive validation of the configuration.
// Returns ValidationResult with any validation errors found.
// This method is designed to be easily replaceable with an external validator.
func (c *Config) Validate() ValidationResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := ValidationResult{Valid: true, Errors: []ValidationError{}}

	// Validate App section
	if c.viper.App != nil {
		validateApp(c.viper.App, &result)
	}

	// Validate Hardware section
	if c.viper.Hardware != nil {
		validateHardware(c.viper.Hardware, &result)
	}

	// Validate Synthesizer section
	if c.viper.Synthesizer != nil {
		validateSynthesizer(c.viper.Synthesizer, &result)
	}

	// Validate Haptics section
	if c.viper.Haptics != nil {
		validateHaptics(c.viper.Haptics, &result)
	}

	// Validate Telemetry section
	if c.viper.Telemetry != nil {
		validateTelemetry(c.viper.Telemetry, &result)
	}

	// Validate PitRadio section
	if c.viper.PitRadio != nil {
		validatePitRadio(c.viper.PitRadio, &result)
	}

	result.Valid = len(result.Errors) == 0

	return result
}

// ValidateConfig validates TOML configuration data without modifying the current config.
// This method is useful for validating imported/uploaded configuration files.
func (c *Config) ValidateConfig(tomlData []byte) ValidationResult {
	// Create a temporary config to validate
	tempViper := viper.New()
	tempViper.SetConfigType("toml")

	err := tempViper.ReadConfig(bytes.NewReader(tomlData))
	if err != nil {
		return ValidationResult{
			Valid: false,
			Errors: []ValidationError{{
				Field:   "config",
				Message: fmt.Sprintf("failed to parse TOML: %v", err),
			}},
		}
	}

	// Unmarshal into our config structure
	var tempConfig viperConfig

	err = tempViper.Unmarshal(&tempConfig)
	if err != nil {
		return ValidationResult{
			Valid: false,
			Errors: []ValidationError{{
				Field:   "config",
				Message: fmt.Sprintf("failed to unmarshal config: %v", err),
			}},
		}
	}

	// Create a temporary Config instance for validation
	tempConfigInstance := &Config{
		viper: &tempConfig,
	}

	// Run validation on the temporary config
	return tempConfigInstance.Validate()
}

// addError is a helper to add validation errors.
func addError(result *ValidationResult, field, message string) {
	result.Errors = append(result.Errors, ValidationError{
		Field:   field,
		Message: message,
	})
}

// validateApp validates the app configuration section.
func validateApp(app *app, result *ValidationResult) {
	// Validate language code (basic check - could be enhanced)
	if app.Language == "" {
		addError(result, "app.language", "language code is required")
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"trace": true, "debug": true, "info": true,
		"warn": true, "error": true, "fatal": true, "panic": true,
	}
	if !validLogLevels[strings.ToLower(app.LogLevel)] {
		addError(result, "app.logLevel", fmt.Sprintf("invalid log level '%s', must be one of: trace, debug, info, warn, error, fatal, panic", app.LogLevel))
	}

	// Validate WebUI port
	if app.WebUIPort < 1 || app.WebUIPort > 65535 {
		addError(result, "app.webUIPort", fmt.Sprintf("port %d is out of valid range (1-65535)", app.WebUIPort))
	}

	// Validate base directory exists if specified
	if app.BaseDir != "" {
		_, err := os.Stat(app.BaseDir)
		if err != nil {
			addError(result, "app.baseDir", "directory does not exist or is not accessible: "+app.BaseDir)
		}
	}
}

// validateHardware validates the hardware configuration section.
func validateHardware(hw *hardware, result *ValidationResult) { //nolint:varnamelen // hw is acceptable
	// Validate hardware model
	validModels := map[string]bool{
		"console":     true,
		"pirateaudio": true,
		"spotpear":    true,
		"waveshare":   true,
	}
	if !validModels[hw.Model] {
		addError(result, "hardware.model", fmt.Sprintf("invalid model '%s', must be one of: console, pirateaudio, spotpear, waveshare", hw.Model))
	}

	// Validate display orientation
	validOrientations := map[int]bool{0: true, 90: true, 180: true, 270: true}
	if !validOrientations[hw.DisplayOrientation] {
		addError(result, "hardware.displayOrientation", fmt.Sprintf("invalid orientation %d, must be 0, 90, 180, or 270", hw.DisplayOrientation))
	}
}

// validateSynthesizer validates the synthesizer configuration section.
func validateSynthesizer(synth *Synthesizer, result *ValidationResult) {
	// Validate sample rates
	if synth.InternalSampleRateHz < 8000 || synth.InternalSampleRateHz > 192000 {
		addError(result, "synthesizer.internalSampleRateHz", fmt.Sprintf("sample rate %d is out of valid range (8000-192000)", synth.InternalSampleRateHz))
	}

	if synth.OutputSampleRateHz < 8000 || synth.OutputSampleRateHz > 192000 {
		addError(result, "synthesizer.outputSampleRateHz", fmt.Sprintf("sample rate %d is out of valid range (8000-192000)", synth.OutputSampleRateHz))
	}

	// Validate gain value
	validateGain := func(field string, value float64) {
		if value < -60.0 || value > 0.0 {
			addError(result, field, fmt.Sprintf("gain %.2f is out of valid range (-60.0 - 0.0)", value))
		}
	}

	validateGain("synthesizer.masterGain", synth.MasterGain)
	validateGain("synthesizer.chassisGain", synth.ChassisGain)
	validateGain("synthesizer.transmissionGain", synth.TransmissionGain)
	validateGain("synthesizer.transmissionGainMinRace", synth.TransmissionGainMinRace)
	validateGain("synthesizer.transmissionGainMinStreet", synth.TransmissionGainMinStreet)
	validateGain("synthesizer.engineGain", synth.EngineGain)

	// Validate EQ bands (8-band parametric EQ)
	if len(synth.EqBands) != 8 {
		addError(result, "synthesizer.eqBands", fmt.Sprintf("EQ must have exactly 8 bands, got %d", len(synth.EqBands)))
	} else {
		for bandID, band := range synth.EqBands {
			// Validate frequency range (10-70 Hz covers haptic range)
			if band.Frequency < 10.0 || band.Frequency > 70.0 {
				addError(result, fmt.Sprintf("synthesizer.eqBands[%d].frequency", bandID),
					fmt.Sprintf("frequency %.2f Hz is out of valid range (10.0-70.0 Hz)", band.Frequency))
			}

			// Validate gain range
			if band.Gain < -12.0 || band.Gain > 6.0 {
				addError(result, fmt.Sprintf("synthesizer.eqBands[%d].gain", bandID),
					fmt.Sprintf("gain %.2f dB is out of valid range (-12.0 to +6.0 dB)", band.Gain))
			}

			// Validate Q factor range
			if band.Q < 0.1 || band.Q > 20.0 {
				addError(result, fmt.Sprintf("synthesizer.eqBands[%d].q", bandID),
					fmt.Sprintf("Q factor %.2f is out of valid range (0.1-20.0)", band.Q))
			}
		}
	}
}

// validateHaptics validates the haptics configuration section.
func validateHaptics(hap *haptics, result *ValidationResult) {
	// Validate curve values
	if hap.DynamicTransmissionCurve < 5 || hap.DynamicTransmissionCurve > 955 {
		addError(result, "haptics.dynamicTransmissionCurve", fmt.Sprintf("curve %d is out of valid range (5-955)", hap.DynamicTransmissionCurve))
	}

	if hap.JerkCurve < 5 || hap.JerkCurve > 955 {
		addError(result, "haptics.jerkCurve", fmt.Sprintf("curve %d is out of valid range (5-955)", hap.JerkCurve))
	}

	if hap.SnapCurve < 5 || hap.SnapCurve > 955 {
		addError(result, "haptics.snapCurve", fmt.Sprintf("curve %d is out of valid range (5-955)", hap.SnapCurve))
	}

	// Validate max values
	if hap.JerkMax < 1 || hap.JerkMax > 200 {
		addError(result, "haptics.jerkMax", fmt.Sprintf("jerkMax %d is out of valid range (1-200)", hap.JerkMax))
	}

	if hap.SnapMax < 1 || hap.SnapMax > 200 {
		addError(result, "haptics.snapMax", fmt.Sprintf("snapMax %d is out of valid range (1-200)", hap.SnapMax))
	}

	// Validate pulse settings
	if hap.PulseMaxAmplitude < 0.0 || hap.PulseMaxAmplitude > 1.0 {
		addError(result, "haptics.pulseMaxAmplitude", fmt.Sprintf("amplitude %.2f is out of valid range (0.0-1.0)", hap.PulseMaxAmplitude))
	}

	if hap.PulseMaxFrequencyHz <= 20 || hap.PulseMaxFrequencyHz > 200 {
		addError(result, "haptics.pulseMaxFrequencyHz", fmt.Sprintf("frequency %.2f is out of valid range (20-200)", hap.PulseMaxFrequencyHz))
	}

	if hap.PulseMinFrequencyHz <= 5 || hap.PulseMinFrequencyHz > 50 {
		addError(result, "haptics.pulseMinFrequencyHz", fmt.Sprintf("frequency %.2f is out of valid range (5-50)", hap.PulseMinFrequencyHz))
	}

	// Validate min < max
	if hap.PulseMinFrequencyHz >= hap.PulseMaxFrequencyHz {
		addError(result, "haptics.pulseMinFrequencyHz", "pulseMinFrequencyHz must be less than pulseMaxFrequencyHz")
	}

	if hap.DynamicTransmissionGforceMax <= 0 {
		addError(result, "haptics.dynamicTransmissionGforceMax", "gforceMax must be greater than 0")
	}
}

// validateFuel validates the fuel configuration section.
func validateFuel(fuel *fuelMonitoring, result *ValidationResult) {
	// Validate lap values are non-negative
	if fuel.PreWarnNotifyLaps < 0 {
		addError(result, "fuel.preWarnNotifyLaps", "laps must be non-negative")
	}

	if fuel.StrategyNotifyLaps < 0 {
		addError(result, "fuel.strategyNotifyLaps", "laps must be non-negative")
	}

	if fuel.RangeSafetyMarginLaps < 0 {
		addError(result, "fuel.rangeSafetyMarginLaps", "laps must be non-negative")
	}

	if fuel.RangeSafetyMarginMeters < 0 {
		addError(result, "fuel.rangeSafetyMarginMeters", "meters must be non-negative")
	}
}

// validateTyres validates the tyres configuration section.
func validateTyres(tyres *tyreMonitoring, result *ValidationResult) {
	// Validate temperature ranges (reasonable for racing tyres: 0-200°C)
	if tyres.TemperatureOptimalCelsius < 0 || tyres.TemperatureOptimalCelsius > 200 {
		addError(result, "tyres.temperatureOptimalCelsius", fmt.Sprintf("temperature %.1f is out of valid range (0-200)", tyres.TemperatureOptimalCelsius))
	}

	if tyres.TemperatureOperatingWindow < 0 || tyres.TemperatureOperatingWindow > 100 {
		addError(result, "tyres.temperatureOperatingWindow", fmt.Sprintf("window %.1f is out of valid range (0-100)", tyres.TemperatureOperatingWindow))
	}

	if tyres.TemperatureMarginCelsius < 0 || tyres.TemperatureMarginCelsius > 50 {
		addError(result, "tyres.temperatureMarginCelsius", fmt.Sprintf("margin %.1f is out of valid range (0-50)", tyres.TemperatureMarginCelsius))
	}
}

// validateTelemetry validates the telemetry configuration section.
func validateTelemetry(tel *Telemetry, result *ValidationResult) {
	// Validate telemetry source
	validSources := map[string]bool{
		"gt7":            true,
		"gtsport":        true,
		"acc":            true,
		"iracing":        true,
		"automobilista2": true,
	}

	if tel.Source == "" {
		addError(result, "telemetry.source", "telemetry source is required")
	} else if !validSources[strings.ToLower(tel.Source)] {
		// Just warn but don't fail - source list may grow
		log.Warn().Str("source", tel.Source).Msg("unknown telemetry source")
	}
}

// validatePitRadio validates the pit radio configuration section.
func validatePitRadio(radio *pitRadio, result *ValidationResult) {
	if radio.MessageSendIntervalMs < 0 {
		addError(result, "pitRadio.messageSendIntervalMs", "interval must be non-negative")
	}

	// If Discord is configured, validate it
	if radio.Discord != nil {
		if radio.Discord.Token != "" && radio.Discord.GuildID == "" {
			addError(result, "pitRadio.discord.guildID", "guildID is required when token is set")
		}
	}

	if radio.FuelMonitoring != nil {
		validateFuel(radio.FuelMonitoring, result)
	}

	if radio.TyreMonitoring != nil {
		validateTyres(radio.TyreMonitoring, result)
	}
}
