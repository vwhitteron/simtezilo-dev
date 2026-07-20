package config //nolint:testpackage // white-box testing for internal config methods

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	appHaptics "github.com/vwhitteron/simtezilo-dev/app/haptics"
)

// snapshotMutators maps every Config method that mutates persisted configuration
// state to a closure that invokes it with a representative argument.
//
// TestSnapshotStaysInSyncAfterMutation uses this registry to verify that every
// mutator leaves c.snapshot consistent with c.viper immediately after returning,
// without relying on a manually-inserted rebuildSnapshot() call anywhere else.
// This is the regression test for the class of bug where a Set/Increase/Decrease
// method writes to c.viper but forgets to refresh the atomic read snapshot,
// leaving GET-style reads stale until something else happens to trigger a rebuild.
//
// Coverage is enforced by TestAllMutatorsAreCovered, which fails if a new
// Set/Increase/Decrease method is added to Config without a corresponding entry
// here (or in nonMutatingMethods) — so this list cannot silently go stale as the
// config surface grows.
var snapshotMutators = map[string]func(cfg *Config){
	// App section
	"SetAppAccent":                     func(cfg *Config) { cfg.SetAppAccent("gb") },
	"SetAppBaseDir":                    func(cfg *Config) { cfg.SetAppBaseDir("/tmp/test") },
	"SetAppVehicleDBFile":              func(cfg *Config) { cfg.SetAppVehicleDBFile("vehicles-test.db") },
	"SetAppLanguage":                   func(cfg *Config) { cfg.SetAppLanguage("en") },
	"SetAppLogLevel":                   func(cfg *Config) { cfg.SetAppLogLevel("debug") },
	"SetAppUpdateAutoCheck":            func(cfg *Config) { cfg.SetAppUpdateAutoCheck(true) },
	"SetAppUpdateAutoInstall":          func(cfg *Config) { cfg.SetAppUpdateAutoInstall(true) },
	"SetAppUpdateCheckIntervalMinutes": func(cfg *Config) { cfg.SetAppUpdateCheckIntervalMinutes(42) },
	"SetAppUpdateChannel":              func(cfg *Config) { cfg.SetAppUpdateChannel("beta") },
	"SetDevToolsEnabled":               func(cfg *Config) { cfg.SetDevToolsEnabled(true) },
	"SetExperimentalFeaturesEnabled":   func(cfg *Config) { cfg.SetExperimentalFeaturesEnabled(true) },

	// Fan section
	"SetFanEnabled":               func(cfg *Config) { cfg.SetFanEnabled(true) },
	"SetFanMode":                  func(cfg *Config) { cfg.SetFanMode("manual") },
	"SetFanDeviceAddress":         func(cfg *Config) { cfg.SetFanDeviceAddress("AA:BB:CC:DD:EE:FF") },
	"SetFanDeviceName":            func(cfg *Config) { cfg.SetFanDeviceName("test-fan") },
	"SetFanCommandTimeoutMs":      func(cfg *Config) { cfg.SetFanCommandTimeoutMs(500) },
	"SetFanMaxSpeedKPH":           func(cfg *Config) { cfg.SetFanMaxSpeedKPH(120) },
	"IncreaseFanMaxSpeedKPH":      func(cfg *Config) { cfg.IncreaseFanMaxSpeedKPH() },
	"DecreaseFanMaxSpeedKPH":      func(cfg *Config) { cfg.DecreaseFanMaxSpeedKPH() },
	"IncreaseFanCommandTimeoutMs": func(cfg *Config) { cfg.IncreaseFanCommandTimeoutMs() },
	"DecreaseFanCommandTimeoutMs": func(cfg *Config) { cfg.DecreaseFanCommandTimeoutMs() },

	// Hardware section
	"SetHardwareModel":      func(cfg *Config) { cfg.SetHardwareModel("rpi") },
	"SetDisplayOrientation": func(cfg *Config) { cfg.SetDisplayOrientation(90) },

	// Haptics section
	"SetHapticsDynamicTransFeedbackEnabled": func(cfg *Config) { cfg.SetHapticsDynamicTransFeedbackEnabled(true) },
	"SetHapticsJerkCurve":                   func(cfg *Config) { cfg.SetHapticsJerkCurve(400) },
	"IncreaseHapticsJerkCurve":              func(cfg *Config) { cfg.IncreaseHapticsJerkCurve() },
	"DecreaseHapticsJerkCurve":              func(cfg *Config) { cfg.DecreaseHapticsJerkCurve() },
	"SetHapticsJerkMax":                     func(cfg *Config) { cfg.SetHapticsJerkMax(90) },
	"IncreaseHapticsJerkMax":                func(cfg *Config) { cfg.IncreaseHapticsJerkMax() },
	"DecreaseHapticsJerkMax":                func(cfg *Config) { cfg.DecreaseHapticsJerkMax() },
	"SetHapticsEnableReplay":                func(cfg *Config) { cfg.SetHapticsEnableReplay(true) },
	"SetHapticsSnapCurve":                   func(cfg *Config) { cfg.SetHapticsSnapCurve(400) },
	"IncreaseHapticsSnapCurve":              func(cfg *Config) { cfg.IncreaseHapticsSnapCurve() },
	"DecreaseHapticsSnapCurve":              func(cfg *Config) { cfg.DecreaseHapticsSnapCurve() },
	"SetHapticsSnapMax":                     func(cfg *Config) { cfg.SetHapticsSnapMax(80) },
	"IncreaseHapticsSnapMax":                func(cfg *Config) { cfg.IncreaseHapticsSnapMax() },
	"DecreaseHapticsSnapMax":                func(cfg *Config) { cfg.DecreaseHapticsSnapMax() },
	"SetHapticsTransmissionCurve":           func(cfg *Config) { cfg.SetHapticsTransmissionCurve(400) },
	"IncreaseHapticsTransmissionCurve":      func(cfg *Config) { cfg.IncreaseHapticsTransmissionCurve() },
	"DecreaseHapticsTransmissionCurve":      func(cfg *Config) { cfg.DecreaseHapticsTransmissionCurve() },
	"SetHapticsTransmissionGforceMax":       func(cfg *Config) { cfg.SetHapticsTransmissionGforceMax(3.5) },
	"IncreaseHapticsTransmissionGforceMax":  func(cfg *Config) { cfg.IncreaseHapticsTransmissionGforceMax() },
	"DecreasehapticsTransmissionGforceMax":  func(cfg *Config) { cfg.DecreasehapticsTransmissionGforceMax() },
	"IncreaseHapticsEnginePrimaryBalance":   func(cfg *Config) { cfg.IncreaseHapticsEnginePrimaryBalance() },
	"DecreaseHapticsEnginePrimaryBalance":   func(cfg *Config) { cfg.DecreaseHapticsEnginePrimaryBalance() },
	"IncreaseHapticsEngineSecondaryBalance": func(cfg *Config) { cfg.IncreaseHapticsEngineSecondaryBalance() },
	"DecreaseHapticsEngineSecondaryBalance": func(cfg *Config) { cfg.DecreaseHapticsEngineSecondaryBalance() },
	"IncreaseHapticsEnginePulseGain":        func(cfg *Config) { cfg.IncreaseHapticsEnginePulseGain() },
	"DecreaseHapticsEnginePulseGain":        func(cfg *Config) { cfg.DecreaseHapticsEnginePulseGain() },
	"IncreaseHapticsEnginePulseScale":       func(cfg *Config) { cfg.IncreaseHapticsEnginePulseScale() },
	"DecreasehapticsEnginePulseScale":       func(cfg *Config) { cfg.DecreasehapticsEnginePulseScale() },
	"IncreaseHapticsPulseMinHz":             func(cfg *Config) { cfg.IncreaseHapticsPulseMinHz() },
	"DecreaseHapticsPulseMinHz":             func(cfg *Config) { cfg.DecreaseHapticsPulseMinHz() },
	"IncreaseHapticsPulseMaxHz":             func(cfg *Config) { cfg.IncreaseHapticsPulseMaxHz() },
	"DecreaseHapticsPulseMaxHz":             func(cfg *Config) { cfg.DecreaseHapticsPulseMaxHz() },
	"SetHapticsTextureMinFrequencyHz":       func(cfg *Config) { cfg.SetHapticsTextureMinFrequencyHz(20) },
	"SetHapticsTextureMaxFrequencyHz":       func(cfg *Config) { cfg.SetHapticsTextureMaxFrequencyHz(150) },
	"SetHapticsPulseMaxAmplitude":           func(cfg *Config) { cfg.SetHapticsPulseMaxAmplitude(0.75) },
	"IncreaseHapticsPulseMaxAmplitude":      func(cfg *Config) { cfg.IncreaseHapticsPulseMaxAmplitude() },
	"DecreaseHapticsPulseMaxAmplitude":      func(cfg *Config) { cfg.DecreaseHapticsPulseMaxAmplitude() },
	"SetHapticsPulseMaxFrequencyHz":         func(cfg *Config) { cfg.SetHapticsPulseMaxFrequencyHz(120) },
	"SetHapticsPulseMinFrequencyHz":         func(cfg *Config) { cfg.SetHapticsPulseMinFrequencyHz(20) },

	// Synth section
	"SetSynthDRXEnabled":                     func(cfg *Config) { cfg.SetSynthDRXEnabled(true) },
	"SetSynthInternalSampleRateHz":           func(cfg *Config) { cfg.SetSynthInternalSampleRateHz(48000) },
	"SetSynthGainIncrement":                  func(cfg *Config) { cfg.SetSynthGainIncrement(0.5) },
	"SetSynthMasterGain":                     func(cfg *Config) { cfg.SetSynthMasterGain(-10) },
	"SetSynthMasterMute":                     func(cfg *Config) { cfg.SetSynthMasterMute(true) },
	"IncreaseSynthMasterGain":                func(cfg *Config) { cfg.IncreaseSynthMasterGain() },
	"DecreaseSynthMasterGain":                func(cfg *Config) { cfg.DecreaseSynthMasterGain() },
	"SetSynthChannelGain":                    func(cfg *Config) { cfg.SetSynthChannelGain(0, -10) },
	"SetSynthChannelMute":                    func(cfg *Config) { cfg.SetSynthChannelMute(0, true) },
	"SetSynthChannelName":                    func(cfg *Config) { cfg.SetSynthChannelName(0, "test-channel") },
	"IncreaseSynthChannelGain":               func(cfg *Config) { cfg.IncreaseSynthChannelGain(0) },
	"DecreaseSynthChannelGain":               func(cfg *Config) { cfg.DecreaseSynthChannelGain(0) },
	"SetSynthRoute":                          func(cfg *Config) { cfg.SetSynthRoute(RoutingSourceEngine, 0, false) },
	"SetSynthChassisMute":                    func(cfg *Config) { cfg.SetSynthChassisMute(true) },
	"SetSynthChassisGain":                    func(cfg *Config) { cfg.SetSynthChassisGain(-10) },
	"IncreaseSynthChassisGain":               func(cfg *Config) { cfg.IncreaseSynthChassisGain() },
	"DecreaseSynthChassisGain":               func(cfg *Config) { cfg.DecreaseSynthChassisGain() },
	"SetSynthTextureMute":                    func(cfg *Config) { cfg.SetSynthTextureMute(true) },
	"SetSynthTextureGain":                    func(cfg *Config) { cfg.SetSynthTextureGain(-10) },
	"IncreaseSynthTextureGain":               func(cfg *Config) { cfg.IncreaseSynthTextureGain() },
	"DecreaseSynthTextureGain":               func(cfg *Config) { cfg.DecreaseSynthTextureGain() },
	"SetSynthTransmissionGainMinRace":        func(cfg *Config) { cfg.SetSynthTransmissionGainMinRace(-10) },
	"IncreaseSynthTransmissionGainMinRace":   func(cfg *Config) { cfg.IncreaseSynthTransmissionGainMinRace() },
	"DecreaseSynthTransmissionGainMinRace":   func(cfg *Config) { cfg.DecreaseSynthTransmissionGainMinRace() },
	"SetSynthTransmissionGainMinStreet":      func(cfg *Config) { cfg.SetSynthTransmissionGainMinStreet(-10) },
	"IncreaseSynthTransmissionGainMinStreet": func(cfg *Config) { cfg.IncreaseSynthTransmissionGainMinStreet() },
	"DecreaseSynthTransmissionGainMinStreet": func(cfg *Config) { cfg.DecreaseSynthTransmissionGainMinStreet() },
	"SetSynthTransmissionMute":               func(cfg *Config) { cfg.SetSynthTransmissionMute(true) },
	"SetSynthTransmissionGain":               func(cfg *Config) { cfg.SetSynthTransmissionGain(-10) },
	"IncreaseSynthTransmissionGain":          func(cfg *Config) { cfg.IncreaseSynthTransmissionGain() },
	"DecreaseSynthTransmissionGain":          func(cfg *Config) { cfg.DecreaseSynthTransmissionGain() },
	"SetSynthEngineMute":                     func(cfg *Config) { cfg.SetSynthEngineMute(true) },
	"SetSynthEngineGain":                     func(cfg *Config) { cfg.SetSynthEngineGain(-10) },
	"IncreaseSynthEngineGain":                func(cfg *Config) { cfg.IncreaseSynthEngineGain() },
	"DecreaseSynthEngineGain":                func(cfg *Config) { cfg.DecreaseSynthEngineGain() },
	"SetSynthEngineProfile": func(cfg *Config) {
		cfg.SetSynthEngineProfile("test-profile", appHaptics.EngineProfile{
			PrimaryBalance:   0.5,
			SecondaryBalance: 0.5,
			Gain:             0.1,
			PulseScale:       1.0,
		})
	},
	"SetSynthChannelEqEnabled": func(cfg *Config) { cfg.SetSynthChannelEqEnabled(0, true) },
	"SetSynthChannelEq": func(cfg *Config) {
		cfg.SetSynthChannelEq(0, []EQBand{
			{Frequency: 12, Gain: 1, Q: 2},
			{Frequency: 16, Gain: 1, Q: 2},
			{Frequency: 20, Gain: 1, Q: 2},
			{Frequency: 25, Gain: 1, Q: 2},
			{Frequency: 30, Gain: 1, Q: 2},
			{Frequency: 38, Gain: 1, Q: 2},
			{Frequency: 48, Gain: 1, Q: 2},
			{Frequency: 58, Gain: 1, Q: 2},
		})
	},

	// Telemetry section
	"SetTelemetrySource":    func(cfg *Config) { cfg.SetTelemetrySource("iracing") },
	"SetTelemetryUpdateURL": func(cfg *Config) { cfg.SetTelemetryUpdateURL("https://example.invalid/update") },

	// Audio section
	"SetAudioHapticsDevice":       func(cfg *Config) { cfg.SetAudioHapticsDevice("hw:1,0") },
	"SetAudioHapticsDeviceName":   func(cfg *Config) { cfg.SetAudioHapticsDeviceName("test-device") },
	"SetAudioHapticsChannels":     func(cfg *Config) { cfg.SetAudioHapticsChannels(4) },
	"SetAudioHapticsSampleRate":   func(cfg *Config) { cfg.SetAudioHapticsSampleRate(48000) },
	"SetAudioHapticsLatencyMs":    func(cfg *Config) { cfg.SetAudioHapticsLatencyMs(20) },
	"SetAudioHapticsCushionMs":    func(cfg *Config) { cfg.SetAudioHapticsCushionMs(10) },
	"SetAudioPitRadioDevice":      func(cfg *Config) { cfg.SetAudioPitRadioDevice("hw:2,0") },
	"SetAudioPitRadioDeviceName":  func(cfg *Config) { cfg.SetAudioPitRadioDeviceName("test-pitradio-device") },
	"SetAudioPitRadioSampleRate":  func(cfg *Config) { cfg.SetAudioPitRadioSampleRate(44100) },
	"SetAudioPitRadioVolume":      func(cfg *Config) { cfg.SetAudioPitRadioVolume(50) },
	"IncreaseAudioPitRadioVolume": func(cfg *Config) { cfg.IncreaseAudioPitRadioVolume() },
	"DecreaseAudioPitRadioVolume": func(cfg *Config) { cfg.DecreaseAudioPitRadioVolume() },

	// Discord section
	"SetDiscordToken":          func(cfg *Config) { cfg.SetDiscordToken("test-token") },
	"SetDiscordGuildID":        func(cfg *Config) { cfg.SetDiscordGuildID("test-guild") },
	"SetDiscordChannelID":      func(cfg *Config) { cfg.SetDiscordChannelID("test-channel") },
	"SetDiscordVoiceChannelID": func(cfg *Config) { cfg.SetDiscordVoiceChannelID("test-voice-channel") },

	// Pit radio section
	"SetPitRadioEnabled":                             func(cfg *Config) { cfg.SetPitRadioEnabled(true) },
	"SetPitRadioOutput":                              func(cfg *Config) { cfg.SetPitRadioOutput("audio") },
	"SetPitRadioMessageSendIntervalMs":               func(cfg *Config) { cfg.SetPitRadioMessageSendIntervalMs(3000) },
	"SetPitRadioNotifyRaceProgressEnabled":           func(cfg *Config) { cfg.SetPitRadioNotifyRaceProgressEnabled(true) },
	"SetPitRadioNotifyRaceProgressMinLaps":           func(cfg *Config) { cfg.SetPitRadioNotifyRaceProgressMinLaps(5) },
	"IncreasePitRadioNotifyRaceProgressMinLaps":      func(cfg *Config) { cfg.IncreasePitRadioNotifyRaceProgressMinLaps() },
	"DecreasePitRadioNotifyRaceProgressMinLaps":      func(cfg *Config) { cfg.DecreasePitRadioNotifyRaceProgressMinLaps() },
	"SetPitRadioNotifyRaceProgressIntervalPc":        func(cfg *Config) { cfg.SetPitRadioNotifyRaceProgressIntervalPc(10) },
	"IncreasePitRadioNotifyRaceProgressIntervalPc":   func(cfg *Config) { cfg.IncreasePitRadioNotifyRaceProgressIntervalPc() },
	"DecreasePitRadioNotifyRaceProgressIntervalPc":   func(cfg *Config) { cfg.DecreasePitRadioNotifyRaceProgressIntervalPc() },
	"SetPitRadioNotifyRaceLapsEnabled":               func(cfg *Config) { cfg.SetPitRadioNotifyRaceLapsEnabled(true) },
	"SetPitRadioNotifyRaceLapsIntervalLaps":          func(cfg *Config) { cfg.SetPitRadioNotifyRaceLapsIntervalLaps(3) },
	"IncreasePitRadioNotifyRaceLapsIntervalLaps":     func(cfg *Config) { cfg.IncreasePitRadioNotifyRaceLapsIntervalLaps() },
	"DecreasePitRadioNotifyRaceLapsIntervalLaps":     func(cfg *Config) { cfg.DecreasePitRadioNotifyRaceLapsIntervalLaps() },
	"SetPitRadioNotifyRaceLapsCountdownLaps":         func(cfg *Config) { cfg.SetPitRadioNotifyRaceLapsCountdownLaps(2) },
	"IncreasePitRadioNotifyRaceLapsCountdownLaps":    func(cfg *Config) { cfg.IncreasePitRadioNotifyRaceLapsCountdownLaps() },
	"DecreasePitRadioNotifyRaceLapsCountdownLaps":    func(cfg *Config) { cfg.DecreasePitRadioNotifyRaceLapsCountdownLaps() },
	"SetPitRadioNotifyLapTimesEnabled":               func(cfg *Config) { cfg.SetPitRadioNotifyLapTimesEnabled(true) },
	"SetPitRadioNotifyLapTimesMaxDeltaSeconds":       func(cfg *Config) { cfg.SetPitRadioNotifyLapTimesMaxDeltaSeconds(1.5) },
	"IncreasePitRadioNotifyLapTimesMaxDeltaSeconds":  func(cfg *Config) { cfg.IncreasePitRadioNotifyLapTimesMaxDeltaSeconds() },
	"DecreasePitRadioNotifyLapTimesMaxDeltaSeconds":  func(cfg *Config) { cfg.DecreasePitRadioNotifyLapTimesMaxDeltaSeconds() },
	"SetPitRadioNotifyCircuitMatchingEnabled":        func(cfg *Config) { cfg.SetPitRadioNotifyCircuitMatchingEnabled(true) },
	"SetPitRadioFuelMonitoringEnabled":               func(cfg *Config) { cfg.SetPitRadioFuelMonitoringEnabled(true) },
	"SetPitRadioFuelPreWarnNotifyLaps":               func(cfg *Config) { cfg.SetPitRadioFuelPreWarnNotifyLaps(3) },
	"IncreasePitRadioFuelPreWarnNotifyLaps":          func(cfg *Config) { cfg.IncreasePitRadioFuelPreWarnNotifyLaps() },
	"DecreasePitRadioFuelPreWarnNotifyLaps":          func(cfg *Config) { cfg.DecreasePitRadioFuelPreWarnNotifyLaps() },
	"SetPitRadioFuelStrategyNotifyLaps":              func(cfg *Config) { cfg.SetPitRadioFuelStrategyNotifyLaps(2) },
	"IncreasePitRadioFuelStrategyNotifyLaps":         func(cfg *Config) { cfg.IncreasePitRadioFuelStrategyNotifyLaps() },
	"DecreasePitRadioFuelStrategyNotifyLaps":         func(cfg *Config) { cfg.DecreasePitRadioFuelStrategyNotifyLaps() },
	"SetPitRadioFuelRangeSafetyMarginLaps":           func(cfg *Config) { cfg.SetPitRadioFuelRangeSafetyMarginLaps(1) },
	"IncreasePitRadioFuelRangeSafetyMarginLaps":      func(cfg *Config) { cfg.IncreasePitRadioFuelRangeSafetyMarginLaps() },
	"DecreasePitRadioFuelRangeSafetyMarginLaps":      func(cfg *Config) { cfg.DecreasePitRadioFuelRangeSafetyMarginLaps() },
	"SetPitRadioFuelRangeSafetyMarginMetres":         func(cfg *Config) { cfg.SetPitRadioFuelRangeSafetyMarginMetres(100) },
	"IncreasePitRadioFuelRangeSafetyMarginMetres":    func(cfg *Config) { cfg.IncreasePitRadioFuelRangeSafetyMarginMetres() },
	"DecreasePitRadioFuelRangeSafetyMarginMetres":    func(cfg *Config) { cfg.DecreasePitRadioFuelRangeSafetyMarginMetres() },
	"SetPitRadioTyreMonitoringEnabled":               func(cfg *Config) { cfg.SetPitRadioTyreMonitoringEnabled(true) },
	"SetPitRadioTyreTemperatureOptimalCelsius":       func(cfg *Config) { cfg.SetPitRadioTyreTemperatureOptimalCelsius(90) },
	"IncreasePitRadioTyreTemperatureOptimalCelsius":  func(cfg *Config) { cfg.IncreasePitRadioTyreTemperatureOptimalCelsius() },
	"DecreasePitRadioTyreTemperatureOptimalCelsius":  func(cfg *Config) { cfg.DecreasePitRadioTyreTemperatureOptimalCelsius() },
	"SetPitRadioTyreTemperatureOperatingWindow":      func(cfg *Config) { cfg.SetPitRadioTyreTemperatureOperatingWindow(15) },
	"IncreasePitRadioTyreTemperatureOperatingWindow": func(cfg *Config) { cfg.IncreasePitRadioTyreTemperatureOperatingWindow() },
	"DecreasePitRadioTyreTemperatureOperatingWindow": func(cfg *Config) { cfg.DecreasePitRadioTyreTemperatureOperatingWindow() },
	"SetPitRadioTyreTemperatureMarginCelsius":        func(cfg *Config) { cfg.SetPitRadioTyreTemperatureMarginCelsius(5) },
	"IncreasePitRadioTyreTemperatureMarginCelsius":   func(cfg *Config) { cfg.IncreasePitRadioTyreTemperatureMarginCelsius() },
	"DecreasePitRadioTyreTemperatureMarginCelsius":   func(cfg *Config) { cfg.DecreasePitRadioTyreTemperatureMarginCelsius() },
}

// nonMutatingMethods lists Set*/Increase*/Decrease* methods on Config that are
// intentionally excluded from snapshotMutators because they configure runtime
// wiring rather than persisted config data, so there is nothing for a snapshot
// to go stale about.
var nonMutatingMethods = map[string]bool{
	"SetDefault": true, // resets viper to defaults directly; not a targeted field mutation
	"SetI18n":    true, // wires an *i18n.I18n dependency, not config data
}

// isMutatorMethodName reports whether name looks like a config-mutating method
// (Set/Increase/Decrease) that TestAllMutatorsAreCovered should require coverage for.
func isMutatorMethodName(name string) bool {
	return strings.HasPrefix(name, "Set") ||
		strings.HasPrefix(name, "Increase") ||
		strings.HasPrefix(name, "Decrease")
}

// TestAllMutatorsAreCovered fails if a new Set/Increase/Decrease method is added
// to Config without being added to snapshotMutators (or explicitly excluded via
// nonMutatingMethods), so new snapshot-sync regressions can't slip in uncovered.
func TestAllMutatorsAreCovered(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[*Config]()
	for i := range typ.NumMethod() {
		name := typ.Method(i).Name

		if !isMutatorMethodName(name) || nonMutatingMethods[name] {
			continue
		}

		if _, ok := snapshotMutators[name]; !ok {
			t.Errorf("Config.%s has no entry in snapshotMutators (config_snapshot_sync_test.go) — "+
				"add coverage there, or to nonMutatingMethods if it doesn't mutate persisted config data", name)
		}
	}
}

// TestSnapshotStaysInSyncAfterMutation calls every registered mutator on a fresh
// Config and checks that the atomic read snapshot already reflects the change —
// i.e. that it matches what a forced rebuildSnapshot() would produce. If a
// mutator writes to c.viper but forgets to refresh c.snapshot, this test catches
// it directly instead of relying on incidental coverage elsewhere.
func TestSnapshotStaysInSyncAfterMutation(t *testing.T) {
	t.Parallel()

	for name, mutate := range snapshotMutators {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := newTestConfig()

			mutate(cfg)
			got := cfg.snapshot.Load()

			cfg.mu.Lock()
			cfg.rebuildSnapshot()
			cfg.mu.Unlock()
			want := cfg.snapshot.Load()

			assert.Equal(t, want, got,
				"Config.%s left the snapshot out of sync with the underlying config "+
					"immediately after returning (missing rebuildSnapshot call?)", name)
		})
	}
}
