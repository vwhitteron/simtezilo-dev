package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics/profiles"
	"github.com/vwhitteron/simtezilo-dev/app/platform"
	"github.com/vwhitteron/simtezilo-dev/app/updater"
)

type configHandler struct {
	log         zerolog.Logger
	config      *appconfig.Config
	calibrator  *calibrator.ToneGenerator
	updater     *updater.Updater
	broadcaster *Broadcaster

	// onHapticsOutputChanged is invoked (if non-nil) after the haptics output
	// device/channels/sampleRate/latency are changed via a config update, so the
	// app can restart the haptic audio stream live. Only called when a value
	// actually changed.
	onHapticsOutputChanged func()
	// bluetoothAvailable reports whether Bluetooth management is available; it is
	// surfaced in the config payload so the UI can show/hide the section. Wired
	// from the system handler, which owns the platform helper.
	bluetoothAvailable func() bool
	// btDevices returns the current paired/connected Bluetooth devices, used to
	// enrich bluealsa audio outputs with their friendly alias. Wired from the
	// system handler, which owns the platform helper. May be nil on builds with
	// no helper.
	btDevices func(context.Context) []platform.CmdBTDevice
	// sendPitRadioTest speaks a short test announcement through the live pit-radio
	// output, used by the audio settings "Test" button to verify the pit-radio
	// audio device. Wired from the app; nil when no pit-radio output is active.
	sendPitRadioTest func() error
	// deriveHapticsChannels resolves the selected haptics device's output channel
	// count. Injectable so tests avoid opening a real audio backend; defaults to
	// the PortAudio-backed implementation (realDeriveHapticsChannels).
	deriveHapticsChannels func() int
}

func newConfigHandler(
	log zerolog.Logger,
	config *appconfig.Config,
	cal *calibrator.ToneGenerator,
	upd *updater.Updater,
	broadcaster *Broadcaster,
	onHapticsOutputChanged func(),
	bluetoothAvailable func() bool,
	btDevices func(context.Context) []platform.CmdBTDevice,
	sendPitRadioTest func() error,
) *configHandler {
	h := &configHandler{
		log:                    log,
		config:                 config,
		calibrator:             cal,
		updater:                upd,
		broadcaster:            broadcaster,
		onHapticsOutputChanged: onHapticsOutputChanged,
		bluetoothAvailable:     bluetoothAvailable,
		btDevices:              btDevices,
		sendPitRadioTest:       sendPitRadioTest,
	}
	h.deriveHapticsChannels = h.realDeriveHapticsChannels

	return h
}

// handleConfigAPI handles GET and POST requests for configuration management.
func (h *configHandler) handleConfigAPI(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	switch request.Method {
	case http.MethodGet:
		h.handleGetConfig(response, request)
	case http.MethodPost:
		h.handleSetConfig(response, request)
	default:
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding
	}
}

// handleGetConfig returns the current configuration as JSON.
func (h *configHandler) handleGetConfig(response http.ResponseWriter, _ *http.Request) {
	configData := map[string]any{
		"app": map[string]any{
			"language":                   *h.config.GetAppLanguage(),
			"accent":                     h.config.GetAppAccent(),
			"logLevel":                   h.config.GetAppLogLevel(),
			"baseDir":                    h.config.GetAppBaseDir(),
			"vehicleDBFile":              h.config.GetAppVehicleDBFile(),
			"enabledWebUI":               h.config.GetAppWebUIEnabled(),
			"webUIPort":                  h.config.GetAppWebUIPort(),
			"enableDevTools":             h.config.GetDevToolsEnabled(),
			"enableExperimentalFeatures": h.config.GetExperimentalFeaturesEnabled(),
			"updates": map[string]any{
				"channel": h.config.GetAppUpdateChannel(),
			},
		},
		"discord": map[string]any{
			"token":          h.config.GetDiscordToken(),
			"guildID":        h.config.GetDiscordGuildID(),
			"channelID":      h.config.GetDiscordChannelID(),
			"voiceChannelID": h.config.GetDiscordVoiceChannelID(),
		},
		"hardware": map[string]any{
			"model":              h.config.GetHardwareModel(),
			"displayOrientation": h.config.GetDisplayOrientation(),
		},
		"bluetooth": map[string]any{
			"available": h.bluetoothAvailable != nil && h.bluetoothAvailable(),
		},
		"haptics": map[string]any{
			"enableReplay": h.config.GetHapticsReplayEnabled(),
			"output": map[string]any{
				"device":     h.config.GetAudioHapticsDevice(),
				"deviceName": h.config.GetAudioHapticsDeviceName(),
				"channels":   h.config.GetAudioHapticsChannels(),
				"sampleRate": h.config.GetAudioHapticsSampleRate(),
				"latencyMs":  h.config.GetAudioHapticsLatencyMs(),
			},
			"dynamicTransmissionFeedback":  h.config.GethapticsDynamicTransFeedbackEnabled(),
			"dynamicTransmissionCurve":     h.config.GetHapticsTransmissionCurve(),
			"dynamicTransmissionGforceMax": h.config.GetHapticsTransmissionGforceMax(),
			"jerkCurve":                    h.config.GethapticsJerkCurve(),
			"jerkMax":                      h.config.GetHapticsJerkMax(),
			"snapCurve":                    h.config.GetHapticsSnapCurve(),
			"snapMax":                      h.config.GetHapticsSnapMax(),
			"pulseMaxAmplitude":            h.config.GetHapticsPulseMaxAmplitude(),
			"pulseMaxFrequencyHz":          h.config.GetHapticsPulseMaxHz(),
			"pulseMinFrequencyHz":          h.config.GetHapticsPulseMinHz(),
			"textureMinFrequencyHz":        h.config.GetHapticsTextureMinHz(),
			"textureMaxFrequencyHz":        h.config.GetHapticsTextureMaxHz(),
		},
		"pitRadio": map[string]any{
			"enabled":               h.config.PitRadioEnabled(),
			"output":                h.config.GetPitRadioOutput(),
			"messageSendIntervalMs": h.config.GetPitRadioMessageSendIntervalMs(),
			"audio": map[string]any{
				"device":     h.config.GetAudioPitRadioDevice(),
				"deviceName": h.config.GetAudioPitRadioDeviceName(),
				"sampleRate": h.config.GetAudioPitRadioSampleRate(),
				"volume":     h.config.GetAudioPitRadioVolume(),
			},
			"notifications": map[string]any{
				"enableRaceProgress":      h.config.GetPitRadioNotifyRaceProgressEnabled(),
				"raceProgressMinLaps":     h.config.GetPitRadioNotifyRaceProgressMinLaps(),
				"raceProgressIntervalPc":  h.config.GetPitRadioNotifyRaceProgressIntervalPc(),
				"enableRaceLaps":          h.config.GetPitRadioNotifyRaceLapsEnabled(),
				"raceLapsIntervalLaps":    h.config.GetPitRadioNotifyRaceLapsIntervalLaps(),
				"raceLapsCountdownLaps":   h.config.GetPitRadioNotifyRaceLapsCountdownLaps(),
				"enableLapTimes":          h.config.GetPitRadioNotifyLapTimesEnabled(),
				"lapTimesMaxDeltaSeconds": h.config.GetPitRadioNotifyLapTimesMaxDeltaSeconds(),
				"enableCircuitMatching":   h.config.GetPitRadioNotifyCircuitMatchingEnabled(),
			},
			"fuelMonitoring": map[string]any{
				"enabled":                 h.config.GetPitRadioFuelMonitoringEnabled(),
				"preWarnNotifyLaps":       h.config.GetPitRadioFuelPreWarnNotifyLaps(),
				"strategyNotifyLaps":      h.config.GetPitRadioFuelStrategyNotifyLaps(),
				"rangeSafetyMarginLaps":   h.config.GetPitRadioFuelRangeSafetyMarginLaps(),
				"rangeSafetyMarginMetres": h.config.GetPitRadioFuelRangeSafetyMarginMetres(),
			},
			"tyreMonitoring": map[string]any{
				"enabled":                    h.config.GetPitRadioTyreMonitoringEnabled(),
				"temperatureOptimalCelsius":  h.config.GetPitRadioTyreTemperatureOptimalCelsius(),
				"temperatureOperatingWindow": h.config.GetPitRadioTyreTemperatureOperatingWindow(),
				"temperatureMarginCelsius":   h.config.GetPitRadioTyreTemperatureMarginCelsius(),
			},
			"discord": map[string]any{
				"token":          h.config.GetDiscordToken(),
				"guildID":        h.config.GetDiscordGuildID(),
				"channelID":      h.config.GetDiscordChannelID(),
				"voiceChannelID": h.config.GetDiscordVoiceChannelID(),
			},
		},
		"synthesizer": map[string]any{
			"internalSampleRateHz": h.config.GetSynthInternalSampleRateHz(),
			"outputFile":           h.config.GetSynthOutputFile(),
			"masterMute":           h.config.GetSynthMasterMute(),
			"masterGain":           h.config.GetSynthMasterGain(),
			"channelMute": func() []bool {
				n := h.config.GetAudioHapticsChannels()

				vals := make([]bool, n)
				for i := range n {
					vals[i] = h.config.GetSynthChannelMute(i)
				}

				return vals
			}(),
			"channelGain": func() []float64 {
				n := h.config.GetAudioHapticsChannels()

				vals := make([]float64, n)
				for i := range n {
					vals[i] = h.config.GetSynthChannelGain(i)
				}

				return vals
			}(),
			"channelName": func() []string {
				n := h.config.GetAudioHapticsChannels()

				vals := make([]string, n)
				for i := range n {
					vals[i] = h.config.GetSynthChannelName(i)
				}

				return vals
			}(),
			"chassisMute":               h.config.GetSynthChassisMute(),
			"chassisGain":               h.config.GetSynthChassisGain(),
			"textureMute":               h.config.GetSynthTextureMute(),
			"textureGain":               h.config.GetSynthTextureGain(),
			"transmissionMute":          h.config.GetSynthTransmissionMute(),
			"transmissionGain":          h.config.GetSynthTransmissionGain(),
			"transmissionGainMinRace":   h.config.GetSynthTransmissionGainMinRace(),
			"transmissionGainMinStreet": h.config.GetSynthTransmissionGainMinStreet(),
			"engineMute":                h.config.GetSynthEngineMute(),
			"engineGain":                h.config.GetSynthEngineGain(),
			"gainIncrement":             h.config.GetSynthGainIncrement(),
			"engineProfiles":            h.config.GetSynthEngineProfiles(),
			"enableEQ":                  h.config.GetSynthChannelsEqEnabled(),
			"enableDrx":                 h.config.GetSynthDRXEnabled(),
			"eq":                        h.config.GetSynthChannelsEq(),
			"routing":                   h.config.GetSynthRouting(),
		},
		"eqCurve": func() map[string]any {
			curves, minFreq, resolution := h.config.GetSynthChannelsEqCurve()

			return map[string]any{
				"curve":      curves,
				"minFreq":    minFreq,
				"resolution": resolution,
			}
		}(),
		"drxHeadroom": func() []float64 {
			n := h.config.GetAudioHapticsChannels()

			vals := make([]float64, n)
			for i := range n {
				vals[i] = h.config.GetSynthChannelDRXHeadroom(i)
			}

			return vals
		}(),
		"telemetry": map[string]any{
			"source":    h.config.GetTelemetrySource(),
			"updateURL": h.config.GetTelemetryUpdateURL(),
		},
		"fan": map[string]any{
			"enabled":          h.config.FanEnabled(),
			"mode":             h.config.GetFanMode(),
			"deviceAddress":    h.config.GetFanDeviceAddress(),
			"deviceName":       h.config.GetFanDeviceName(),
			"commandTimeoutMs": h.config.GetFanCommandTimeoutMs(),
			"maxSpeedKph":      h.config.GetFanMaxSpeedKPH(),
		},
		"calibration": map[string]any{
			"enabled":       h.calibrator.IsEnabled(),
			"frequency":     h.calibrator.GetSweepFrequency(),
			"sweeping":      h.calibrator.IsSweeping(),
			"sweepMin":      h.calibrator.GetSweepMin(),
			"sweepMax":      h.calibrator.GetSweepMax(),
			"sweepDuration": h.calibrator.GetSweepDuration(),
		},
	}

	err := json.NewEncoder(response).Encode(configData)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode config JSON")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to encode configuration"}) //nolint:errchkjson // simple encoding
	}
}

// handleSetConfig updates the configuration from JSON data.
func (h *configHandler) handleSetConfig(response http.ResponseWriter, request *http.Request) {
	var configData map[string]any

	err := json.NewDecoder(request.Body).Decode(&configData)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to decode config JSON")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Invalid JSON data"}) //nolint:errchkjson // simple encoding

		return
	}

	h.log.Debug().Interface("configData", configData).Msg("received config update request")

	restartRequired := h.checkRestartRequired(configData)

	errors := h.applyConfigChanges(configData)

	if len(errors) > 0 {
		h.log.Error().Strs("errors", errors).Msg("failed to apply some configuration changes")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":  "partial_success",
			"message": "Some configuration changes failed",
			"errors":  errors,
		})

		return
	}

	h.log.Debug().Interface("config", configData).Msg("configuration updated successfully")

	err = h.config.SaveConfigToFile()
	if err != nil {
		h.log.Error().Err(err).Msg("failed to save configuration to file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Configuration updated but failed to save: " + err.Error(),
		})

		return
	}

	h.log.Debug().Msg("configuration saved to file")

	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"status":          "success",
		"message":         "Configuration updated and saved successfully",
		"restartRequired": restartRequired,
		"config": map[string]any{
			"eqCurve": func() map[string]any {
				curves, minFreq, resolution := h.config.GetSynthChannelsEqCurve()

				return map[string]any{
					"curve":      curves,
					"minFreq":    minFreq,
					"resolution": resolution,
				}
			}(),
			"drxHeadroom": []float64{
				h.config.GetSynthChannelDRXHeadroom(0),
				h.config.GetSynthChannelDRXHeadroom(1),
			},
			"calibration": map[string]any{
				"enabled":       h.calibrator.IsEnabled(),
				"frequency":     h.calibrator.GetSweepFrequency(),
				"sweeping":      h.calibrator.IsSweeping(),
				"sweepMin":      h.calibrator.GetSweepMin(),
				"sweepMax":      h.calibrator.GetSweepMax(),
				"sweepDuration": h.calibrator.GetSweepDuration(),
			},
		},
	})
}

// applyConfigChanges applies the configuration changes using appropriate setter methods.
func (h *configHandler) applyConfigChanges(configData map[string]any) []string {
	dispatch := map[string]func(map[string]any) []string{
		"app":         h.applyAppConfig,
		"synthesizer": h.applySynthesizerConfig,
		"haptics":     h.applyHapticsConfig,
		"hardware":    h.applyHardwareConfig,
		"telemetry":   h.applyTelemetryConfig,
		"discord":     h.applyDiscordConfig,
		"calibration": h.applyCalibrationConfig,
		"pitRadio":    h.applyPitRadioConfig,
		"fan":         h.applyFanConfig,
	}

	var errors []string

	for section, sectionData := range configData {
		sectionMap, ok := sectionData.(map[string]any)
		if !ok {
			errors = append(errors, "invalid data for section "+section)

			continue
		}

		applyFn, known := dispatch[section]
		if !known {
			h.log.Debug().Str("section", section).Msg("configuration section not implemented for updates")

			continue
		}

		errors = append(errors, applyFn(sectionMap)...)
	}

	return errors
}

// applyAppConfig applies application configuration changes.
func (h *configHandler) applyAppConfig(config map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(config, "language", "invalid language value", h.config.SetAppLanguage))
	errors = appendErr(errors, applyField(config, "accent", "invalid accent value", h.config.SetAppAccent))
	errors = appendErr(errors, applyField(config, "logLevel", "invalid log level value", h.config.SetAppLogLevel))
	errors = appendErr(errors, applyField(config, "baseDir", "invalid base directory value", h.config.SetAppBaseDir))
	errors = appendErr(errors, applyField(config, "enableDevTools", "invalid enableDevTools value", h.config.SetDevToolsEnabled))
	errors = appendErr(errors, applyField(config, "enableExperimentalFeatures", "invalid enableExperimentalFeatures value", h.config.SetExperimentalFeaturesEnabled))

	errors = append(errors, applySubMap(config, "updates", "invalid updates configuration", h.applyUpdatesConfig)...)

	return append(errors, h.applyVehicleDBFile(config)...)
}

// applyUpdatesConfig applies the nested updates sub-map.
func (h *configHandler) applyUpdatesConfig(cfg map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(cfg, "autoCheck", "invalid autoCheck value", h.config.SetAppUpdateAutoCheck))
	errors = appendErr(errors, applyField(cfg, "autoInstall", "invalid autoInstall value", h.config.SetAppUpdateAutoInstall))
	errors = appendErr(errors, applyField(cfg, "checkIntervalMinutes", "invalid checkIntervalMinutes value", func(f float64) {
		h.config.SetAppUpdateCheckIntervalMinutes(int(f))
	}))

	errors = appendErr(errors, applyField(cfg, "channel", "invalid channel value", func(channelStr string) {
		h.config.SetAppUpdateChannel(channelStr)
		// TODO: this should probably be done outside of the config update function
		if h.updater != nil {
			h.updater.SetChannel(channelStr)
			h.updater.CheckExistingDownloads()
		}
	}))

	return errors
}

// applyVehicleDBFile validates and applies the vehicleDBFile config key.
func (h *configHandler) applyVehicleDBFile(config map[string]any) []string {
	vehicleDBFile, fileOK := config["vehicleDBFile"]
	if !fileOK {
		return nil
	}

	vehicleDBFileStr, strOK := vehicleDBFile.(string)
	if !strOK {
		return []string{"invalid vehicle database file value"}
	}

	if vehicleDBFileStr != "" {
		_, err := os.Stat(vehicleDBFileStr)
		if err != nil {
			if os.IsNotExist(err) {
				return []string{"vehicle database file not found: " + vehicleDBFileStr}
			}

			return []string{"cannot access vehicle database file: " + err.Error()}
		}
	}

	h.config.SetAppVehicleDBFile(vehicleDBFileStr)

	return nil
}

// applySynthesizerConfig applies synthesizer configuration changes.
func (h *configHandler) applySynthesizerConfig(config map[string]any) []string {
	h.log.Debug().Interface("synthConfig", config).Msg("applying synthesizer configuration")

	var errors []string

	errors = append(errors, h.applySynthSampleRates(config)...)
	errors = append(errors, h.applySynthGainMuteFields(config)...)
	errors = append(errors, applySubMap(config, "engineProfiles", "invalid engine profiles format", h.applyEngineProfiles)...)
	errors = append(errors, h.applyEQEnabled(config)...)
	errors = appendErr(errors, applyField(config, "enableDrx", "invalid DRX enabled value (expected bool)", h.config.SetSynthDRXEnabled))
	errors = append(errors, h.applyEQBands(config)...)
	errors = append(errors, h.applySynthRouting(config)...)

	return errors
}

// applySynthSampleRates applies the internalSampleRateHz field with debug logging.
func (h *configHandler) applySynthSampleRates(config map[string]any) []string {
	var errors []string

	if val, ok := config["internalSampleRateHz"]; ok {
		h.log.Debug().Interface("value", val).Type("type", val).Msg("processing internalSampleRateHz")

		if rateFloat, ok := val.(float64); ok {
			h.config.SetSynthInternalSampleRateHz(int(rateFloat))
			h.log.Debug().Int("rate", int(rateFloat)).Msg("set internal sample rate")
		} else {
			errors = append(errors, "invalid internal sample rate value")

			h.log.Error().Interface("value", val).Msg("invalid internal sample rate value type")
		}
	}

	return errors
}

// applySynthGainMuteFields applies all the scalar gain/mute/increment fields.
func (h *configHandler) applySynthGainMuteFields(config map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(config, "masterGain", "invalid master gain value", h.config.SetSynthMasterGain))
	errors = appendErr(errors, applyField(config, "masterMute", "invalid master gain mute value", h.config.SetSynthMasterMute))
	errors = append(errors, h.applyChannelGainMute(config)...)
	errors = appendErr(errors, applyField(config, "chassisGain", "invalid chassis gain value", h.config.SetSynthChassisGain))
	errors = appendErr(errors, applyField(config, "chassisMute", "invalid chassis gain mute value", h.config.SetSynthChassisMute))
	errors = appendErr(errors, applyField(config, "textureGain", "invalid texture gain value", h.config.SetSynthTextureGain))
	errors = appendErr(errors, applyField(config, "textureMute", "invalid texture gain mute value", h.config.SetSynthTextureMute))
	errors = appendErr(errors, applyField(config, "transmissionGain", "invalid transmission gain value", h.config.SetSynthTransmissionGain))
	errors = appendErr(errors, applyField(config, "transmissionMute", "invalid transmission gain mute value", h.config.SetSynthTransmissionMute))
	errors = appendErr(errors, applyField(config, "transmissionGainMinRace", "invalid transmission gain min race value", h.config.SetSynthTransmissionGainMinRace))
	errors = appendErr(errors, applyField(config, "transmissionGainMinStreet", "invalid transmission gain min street value", h.config.SetSynthTransmissionGainMinStreet))
	errors = appendErr(errors, applyField(config, "engineGain", "invalid engine gain value", h.config.SetSynthEngineGain))
	errors = appendErr(errors, applyField(config, "engineMute", "invalid engine gain mute value", h.config.SetSynthEngineMute))
	errors = appendErr(errors, applyField(config, "gainIncrement", "invalid gain increment value", h.config.SetSynthGainIncrement))

	return errors
}

// applyEngineProfiles applies the engineProfiles sub-map.
func (h *configHandler) applyEngineProfiles(profilesMap map[string]any) []string {
	for name, profileData := range profilesMap {
		if profileMap, ok := profileData.(map[string]any); ok {
			profile := profiles.EngineProfile{}

			if pb, ok := profileMap["primaryBalance"].(float64); ok {
				profile.PrimaryBalance = pb
			}

			if sb, ok := profileMap["secondaryBalance"].(float64); ok {
				profile.SecondaryBalance = sb
			}

			if g, ok := profileMap["gain"].(float64); ok {
				profile.Gain = g
			}

			if ps, ok := profileMap["pulseScale"].(float64); ok {
				profile.PulseScale = ps
			}

			h.config.SetSynthEngineProfile(name, profile)
		}
	}

	return nil
}

// applyChannelGainMute applies the per-channel channelGain and channelMute
// arrays. Each index maps to an output channel; out-of-range indices are
// ignored by the config setters.
func (h *configHandler) applyChannelGainMute(config map[string]any) []string {
	var errors []string

	if gains, found := config["channelGain"]; found {
		gainArray, valid := gains.([]any)
		if !valid {
			errors = append(errors, "invalid channelGain value (expected array)")
		} else {
			for channel, val := range gainArray {
				if gain, ok := val.(float64); ok {
					h.config.SetSynthChannelGain(channel, gain)
				} else {
					errors = append(errors, fmt.Sprintf("invalid gain value for channel %d", channel))
				}
			}
		}
	}

	if mutes, found := config["channelMute"]; found {
		muteArray, valid := mutes.([]any)
		if !valid {
			errors = append(errors, "invalid channelMute value (expected array)")
		} else {
			for channel, val := range muteArray {
				if mute, ok := val.(bool); ok {
					h.config.SetSynthChannelMute(channel, mute)
				} else {
					errors = append(errors, fmt.Sprintf("invalid mute value for channel %d", channel))
				}
			}
		}
	}

	if names, found := config["channelName"]; found {
		nameArray, valid := names.([]any)
		if !valid {
			errors = append(errors, "invalid channelName value (expected array)")
		} else {
			for channel, val := range nameArray {
				if name, ok := val.(string); ok {
					h.config.SetSynthChannelName(channel, name)
				} else {
					errors = append(errors, fmt.Sprintf("invalid name value for channel %d", channel))
				}
			}
		}
	}

	return errors
}

// applyEQEnabled applies the enableEQ array field.
func (h *configHandler) applyEQEnabled(config map[string]any) []string {
	eqEnabled, found := config["enableEQ"]
	if !found {
		return nil
	}

	enabledArray, valid := eqEnabled.([]any)
	if !valid {
		return []string{"invalid EQ enabled value (expected array)"}
	}

	var errors []string

	for channel, val := range enabledArray {
		if enabled, ok := val.(bool); ok {
			h.config.SetSynthChannelEqEnabled(channel, enabled)
		} else {
			errors = append(errors, fmt.Sprintf("invalid EQ enabled value for channel %d", channel))
		}
	}

	return errors
}

// applyEQBands applies the eq channel-band array field.
func (h *configHandler) applyEQBands(config map[string]any) []string {
	eq, found := config["eq"]
	if !found {
		return nil
	}

	channelArray, valid := eq.([]any)
	if !valid {
		return []string{"invalid EQ format (expected array of channel arrays)"}
	}

	var errors []string

	for channel, channelVal := range channelArray {
		errors = append(errors, h.applyEQChannel(channel, channelVal)...)
	}

	return errors
}

// applyEQChannel applies one channel's EQ band array.
func (h *configHandler) applyEQChannel(channel int, channelVal any) []string {
	eqArray, ok := channelVal.([]any)
	if !ok {
		return []string{fmt.Sprintf("invalid EQ format for channel %d", channel)}
	}

	var errors []string

	eqBands := make([]appconfig.EQBand, 0, len(eqArray))

	for idx, val := range eqArray {
		bandMap, ok := val.(map[string]any)
		if !ok {
			errors = append(errors, fmt.Sprintf("invalid EQ band %d format for channel %d", idx+1, channel))

			continue
		}

		freq, freqOk := bandMap["frequency"].(float64)
		gain, gainOk := bandMap["gain"].(float64)
		qVal, qOk := bandMap["q"].(float64)

		if !freqOk || !gainOk || !qOk {
			errors = append(errors, fmt.Sprintf("invalid EQ band %d for channel %d: missing or invalid fields", idx+1, channel))

			continue
		}

		eqBands = append(eqBands, appconfig.EQBand{Frequency: freq, Gain: gain, Q: qVal})
	}

	if len(errors) == 0 {
		if len(eqBands) == 8 {
			h.config.SetSynthChannelEq(channel, eqBands)
		} else {
			errors = append(errors, fmt.Sprintf("EQ for channel %d must have exactly 8 bands, got %d", channel, len(eqBands)))
		}
	}

	return errors
}

// applySynthRouting applies the synthesizer output routing matrix.
// Each provided source row is validated to match the current channel count before
// being applied cell-by-cell. Routing is not a stream-restart change.
func (h *configHandler) applySynthRouting(config map[string]any) []string {
	raw, found := config["routing"]
	if !found {
		return nil
	}

	routingMap, ok := raw.(map[string]any)
	if !ok {
		return []string{"invalid routing value (expected object)"}
	}

	numChannels := h.config.GetAudioHapticsChannels()

	var errors []string

	for source, rowRaw := range routingMap {
		rowAny, ok := rowRaw.([]any)
		if !ok {
			errors = append(errors, fmt.Sprintf("invalid routing row for source %q (expected array)", source))

			continue
		}

		if len(rowAny) != numChannels {
			errors = append(errors, fmt.Sprintf(
				"routing row for source %q has %d entries but channel count is %d",
				source, len(rowAny), numChannels,
			))

			continue
		}

		for ch, valRaw := range rowAny {
			enabled, ok := valRaw.(bool)
			if !ok {
				errors = append(errors, fmt.Sprintf("invalid routing value for source %q channel %d (expected bool)", source, ch))

				continue
			}

			h.config.SetSynthRoute(source, ch, enabled)
		}
	}

	return errors
}

// applyHapticsConfig applies haptics configuration changes.
func (h *configHandler) applyHapticsConfig(config map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(config, "dynamicTransmissionFeedback", "invalid dynamic transmission feedback value", h.config.SetHapticsDynamicTransFeedbackEnabled))
	errors = appendErr(errors, applyField(config, "jerkCurve", "invalid jerk curve value", func(f float64) {
		h.config.SetHapticsJerkCurve(int(math.Round(f * 1000.0)))
	}))
	errors = appendErr(errors, applyField(config, "jerkMax", "invalid jerk max value", func(f float64) {
		h.config.SetHapticsJerkMax(int(f))
	}))
	errors = appendErr(errors, applyField(config, "snapCurve", "invalid snap curve value", func(f float64) {
		h.config.SetHapticsSnapCurve(int(math.Round(f * 1000.0)))
	}))
	errors = appendErr(errors, applyField(config, "snapMax", "invalid snap max value", func(f float64) {
		h.config.SetHapticsSnapMax(int(f))
	}))
	errors = appendErr(errors, applyField(config, "dynamicTransmissionCurve", "invalid transmission curve value", func(f float64) {
		h.config.SetHapticsTransmissionCurve(int(math.Round(f * 1000.0)))
	}))
	errors = appendErr(errors, applyField(config, "dynamicTransmissionGforceMax", "invalid transmission G-force max value", h.config.SetHapticsTransmissionGforceMax))
	errors = appendErr(errors, applyField(config, "pulseMaxAmplitude", "invalid pulse max amplitude value", h.config.SetHapticsPulseMaxAmplitude))
	errors = appendErr(errors, applyField(config, "pulseMaxFrequencyHz", "invalid pulse max frequency value", h.config.SetHapticsPulseMaxFrequencyHz))
	errors = appendErr(errors, applyField(config, "pulseMinFrequencyHz", "invalid pulse min frequency value", h.config.SetHapticsPulseMinFrequencyHz))
	errors = appendErr(errors, applyField(config, "textureMinFrequencyHz", "invalid texture min frequency value", h.config.SetHapticsTextureMinFrequencyHz))
	errors = appendErr(errors, applyField(config, "textureMaxFrequencyHz", "invalid texture max frequency value", h.config.SetHapticsTextureMaxFrequencyHz))
	errors = appendErr(errors, applyField(config, "enableReplay", "invalid enable replay value", h.config.SetHapticsEnableReplay))
	errors = append(errors, applySubMap(config, "output", "invalid haptics output configuration structure", h.applyHapticsOutputConfig)...)

	return errors
}

// applyCalibrationConfig applies calibration configuration changes.
func (h *configHandler) applyCalibrationConfig(config map[string]any) []string {
	var errors []string

	errors = append(errors, h.applyCalibrationEnabled(config)...)
	errors = appendErr(errors, applyField(config, "frequency", "invalid calibration frequency value", h.calibrator.SetFrequency))
	errors = appendErr(errors, applyField(config, "sweepMin", "invalid calibration sweepMin value", h.calibrator.SetSweepMin))
	errors = appendErr(errors, applyField(config, "sweepMax", "invalid calibration sweepMax value", h.calibrator.SetSweepMax))
	errors = appendErr(errors, applyField(config, "sweepDuration", "invalid calibration sweepDuration value", h.calibrator.SetSweepDuration))

	return errors
}

// applyCalibrationEnabled sets the calibration enabled flag and broadcasts the new state.
func (h *configHandler) applyCalibrationEnabled(config map[string]any) []string {
	enabled, found := config["enabled"]
	if !found {
		return nil
	}

	enabledBool, valid := enabled.(bool)
	if !valid {
		return []string{"invalid calibration enabled value"}
	}

	h.calibrator.SetEnabled(enabledBool)

	calibrationState := map[string]any{
		"enabled":       h.calibrator.IsEnabled(),
		"frequency":     h.calibrator.GetSweepFrequency(),
		"volume":        h.calibrator.GetGain(),
		"channel":       string(h.calibrator.GetChannel()),
		"sweeping":      h.calibrator.IsSweeping(),
		"sweepMin":      h.calibrator.GetSweepMin(),
		"sweepMax":      h.calibrator.GetSweepMax(),
		"sweepDuration": h.calibrator.GetSweepDuration(),
	}

	msg := WSMessage{
		Type:      "calibration",
		Timestamp: time.Now().UnixMilli(),
		Data:      calibrationState,
	}

	encodedData, err := json.Marshal(msg)
	if err == nil {
		h.broadcaster.broadcast(encodedData, "calibration")
	}

	return nil
}

// checkRestartRequired checks if any configuration changes require a restart.
func (h *configHandler) checkRestartRequired(configData map[string]any) bool {
	if appConfig, ok := configData["app"].(map[string]any); ok { //nolint:nestif // compact nesting
		if vehicleDBFile, ok := appConfig["vehicleDBFile"]; ok {
			if vehicleDBFileStr, ok := vehicleDBFile.(string); ok {
				if vehicleDBFileStr != h.config.GetAppVehicleDBFile() {
					return true
				}
			}
		}
	}

	if telemetryConfig, ok := configData["telemetry"].(map[string]any); ok { //nolint:nestif // compact nesting
		if source, ok := telemetryConfig["source"]; ok {
			if sourceStr, ok := source.(string); ok {
				if sourceStr != h.config.GetTelemetrySource() {
					return true
				}
			}
		}
	}

	return false
}

// applyFuelConfig applies fuel management configuration changes.
func (h *configHandler) applyFuelConfig(config map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(config, "enabled", "invalid fuel monitoring enabled value", h.config.SetPitRadioFuelMonitoringEnabled))
	errors = appendErr(errors, applyField(config, "preWarnNotifyLaps", "invalid pre-warn notify laps value", h.config.SetPitRadioFuelPreWarnNotifyLaps))
	errors = appendErr(errors, applyField(config, "strategyNotifyLaps", "invalid strategy notify laps value", h.config.SetPitRadioFuelStrategyNotifyLaps))
	errors = appendErr(errors, applyField(config, "rangeSafetyMarginLaps", "invalid range safety margin laps value", h.config.SetPitRadioFuelRangeSafetyMarginLaps))
	errors = appendErr(errors, applyField(config, "rangeSafetyMarginMetres", "invalid range safety margin metres value", h.config.SetPitRadioFuelRangeSafetyMarginMetres))

	return errors
}

// applyHardwareConfig applies hardware configuration changes.
func (h *configHandler) applyHardwareConfig(config map[string]any) []string {
	var errors []string

	if model, ok := config["model"]; ok {
		if modelStr, ok := model.(string); ok {
			h.config.SetHardwareModel(modelStr)
		} else {
			errors = append(errors, "invalid hardware model value")
		}
	}

	if orientation, ok := config["displayOrientation"]; ok {
		if orientFloat, ok := orientation.(float64); ok {
			h.config.SetDisplayOrientation(int(orientFloat))
		} else {
			errors = append(errors, "invalid display orientation value")
		}
	}

	return errors
}

// applyTelemetryConfig applies telemetry configuration changes.
func (h *configHandler) applyTelemetryConfig(config map[string]any) []string {
	var errors []string

	source, cfgOK := config["source"]
	if !cfgOK {
		return errors
	}

	sourceStr, strOK := source.(string)
	if !strOK {
		errors = append(errors, "invalid telemetry source value")

		return errors
	}

	if after, cutOK := strings.CutPrefix(sourceStr, "file://"); cutOK {
		filePath := after

		_, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				errors = append(errors, "telemetry replay file not found: "+filePath)
			} else {
				errors = append(errors, "cannot access telemetry replay file: "+err.Error())
			}

			return errors
		}
	}

	h.config.SetTelemetrySource(sourceStr)

	return errors
}

// applyDiscordConfig applies Discord configuration changes.
func (h *configHandler) applyDiscordConfig(config map[string]any) []string {
	var errors []string

	if token, ok := config["token"]; ok {
		if tokenStr, ok := token.(string); ok {
			h.config.SetDiscordToken(tokenStr)
		} else {
			errors = append(errors, "invalid discord token value")
		}
	}

	if guildID, ok := config["guildID"]; ok {
		if guildIDStr, ok := guildID.(string); ok {
			h.config.SetDiscordGuildID(guildIDStr)
		} else {
			errors = append(errors, "invalid discord guild ID value")
		}
	}

	if channelID, ok := config["channelID"]; ok {
		if channelIDStr, ok := channelID.(string); ok {
			h.config.SetDiscordChannelID(channelIDStr)
		} else {
			errors = append(errors, "invalid discord channel ID value")
		}
	}

	if voiceChannelID, ok := config["voiceChannelID"]; ok {
		if voiceChannelIDStr, ok := voiceChannelID.(string); ok {
			h.config.SetDiscordVoiceChannelID(voiceChannelIDStr)
		} else {
			errors = append(errors, "invalid discord voice channel ID value")
		}
	}

	return errors
}

// applyNotificationsConfig applies notifications configuration changes.
func (h *configHandler) applyNotificationsConfig(config map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(config, "enableRaceProgress", "invalid race progress enabled value", h.config.SetPitRadioNotifyRaceProgressEnabled))
	errors = appendErr(errors, applyField(config, "raceProgressMinLaps", "invalid race progress min laps value", func(f float64) {
		h.config.SetPitRadioNotifyRaceProgressMinLaps(int(f))
	}))
	errors = appendErr(errors, applyField(config, "raceProgressIntervalPc", "invalid race progress interval percentage value", func(f float64) {
		h.config.SetPitRadioNotifyRaceProgressIntervalPc(int(f))
	}))
	errors = appendErr(errors, applyField(config, "enableRaceLaps", "invalid race laps enabled value", h.config.SetPitRadioNotifyRaceLapsEnabled))
	errors = appendErr(errors, applyField(config, "raceLapsIntervalLaps", "invalid race laps interval laps value", func(f float64) {
		h.config.SetPitRadioNotifyRaceLapsIntervalLaps(int(f))
	}))
	errors = appendErr(errors, applyField(config, "raceLapsCountdownLaps", "invalid race laps countdown laps value", func(f float64) {
		h.config.SetPitRadioNotifyRaceLapsCountdownLaps(int(f))
	}))
	errors = appendErr(errors, applyField(config, "enableLapTimes", "invalid lap times enabled value", h.config.SetPitRadioNotifyLapTimesEnabled))
	errors = appendErr(errors, applyField(config, "lapTimesMaxDeltaSeconds", "invalid lap times max delta seconds value", h.config.SetPitRadioNotifyLapTimesMaxDeltaSeconds))
	errors = appendErr(errors, applyField(config, "enableCircuitMatching", "invalid circuit matching enabled value", h.config.SetPitRadioNotifyCircuitMatchingEnabled))

	return errors
}

// applyPitRadioConfig applies pit radio configuration changes.
func (h *configHandler) applyPitRadioConfig(config map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(config, "enabled", "invalid pit radio enabled value", h.config.SetPitRadioEnabled))
	errors = appendErr(errors, applyField(config, "output", "invalid pit radio output value", h.config.SetPitRadioOutput))
	errors = appendErr(errors, applyField(config, "messageSendIntervalMs", "invalid message send interval value", func(f float64) {
		h.config.SetPitRadioMessageSendIntervalMs(int(f))
	}))
	errors = append(errors, applySubMap(config, "audio", "invalid pit radio audio configuration structure", h.applyPitRadioAudioConfig)...)
	errors = append(errors, applySubMap(config, "notifications", "invalid notifications configuration structure", h.applyNotificationsConfig)...)
	errors = append(errors, applySubMap(config, "discord", "invalid discord configuration structure", h.applyDiscordConfig)...)
	errors = append(errors, applySubMap(config, "fuelMonitoring", "invalid fuel monitoring configuration structure", h.applyFuelConfig)...)
	errors = append(errors, applySubMap(config, "tyreMonitoring", "invalid tyre monitoring configuration structure", h.applyTyresConfig)...)

	return errors
}

// applyFanConfig applies fan device configuration changes.
func (h *configHandler) applyFanConfig(config map[string]any) []string {
	var errors []string

	errors = appendErr(errors, applyField(config, "enabled", "invalid fan enabled value", h.config.SetFanEnabled))
	errors = appendErr(errors, applyField(config, "mode", "invalid fan mode value", h.config.SetFanMode))
	errors = appendErr(errors, applyField(config, "deviceAddress", "invalid fan device address value", h.config.SetFanDeviceAddress))
	errors = appendErr(errors, applyField(config, "deviceName", "invalid fan device name value", h.config.SetFanDeviceName))
	errors = appendErr(errors, applyField(config, "commandTimeoutMs", "invalid fan command timeout value", func(f float64) {
		h.config.SetFanCommandTimeoutMs(int(f))
	}))
	errors = appendErr(errors, applyField(config, "maxSpeedKph", "invalid fan max speed value", func(f float64) {
		h.config.SetFanMaxSpeedKPH(int(f))
	}))

	return errors
}

// applyTyresConfig applies tyre management configuration changes.
func (h *configHandler) applyTyresConfig(config map[string]any) []string {
	var errors []string

	if monitoringEnabled, ok := config["enabled"]; ok {
		if enabledBool, ok := monitoringEnabled.(bool); ok {
			h.config.SetPitRadioTyreMonitoringEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid tyre monitoring enabled value")
		}
	}

	if tempOptimal, ok := config["temperatureOptimalCelsius"]; ok {
		if tempFloat, ok := tempOptimal.(float64); ok {
			h.config.SetPitRadioTyreTemperatureOptimalCelsius(float32(tempFloat))
		} else {
			errors = append(errors, "invalid temperature optimal value")
		}
	}

	if tempWindow, ok := config["temperatureOperatingWindow"]; ok {
		if windowFloat, ok := tempWindow.(float64); ok {
			h.config.SetPitRadioTyreTemperatureOperatingWindow(float32(windowFloat))
		} else {
			errors = append(errors, "invalid temperature operating window value")
		}
	}

	if tempMargin, ok := config["temperatureMarginCelsius"]; ok {
		if marginFloat, ok := tempMargin.(float64); ok {
			h.config.SetPitRadioTyreTemperatureMarginCelsius(float32(marginFloat))
		} else {
			errors = append(errors, "invalid temperature margin value")
		}
	}

	return errors
}

// handleConfigStatus returns the configuration status including last update timestamp and restart required flag.
func (h *configHandler) handleConfigStatus(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	status := h.config.Status()

	statusData := map[string]any{
		"lastUpdate":      status.LastUpdate,
		"restartRequired": status.RestartRequired,
	}

	err := json.NewEncoder(response).Encode(statusData)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to encode config status JSON")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to encode status"}) //nolint:errchkjson // simple encoding
	}
}

// handleConfigReset resets the configuration to default values.
func (h *configHandler) handleConfigReset(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	h.log.Info().Msg("configuration reset requested")

	h.config.SetDefault()

	err := h.config.SaveConfigToFile()
	if err != nil {
		h.log.Error().Err(err).Msg("failed to save default configuration to file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to save configuration"}) //nolint:errchkjson // simple encoding

		return
	}

	h.config.MarkRestartRequired()

	h.handleGetConfig(response, request)
}

// handleConfigExport handles GET requests to export the full configuration file from disk.
func (h *configHandler) handleConfigExport(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	configFilePath := h.config.GetConfigFilePath()

	configData, err := os.ReadFile(configFilePath)
	if err != nil {
		h.log.Error().Err(err).Str("file", configFilePath).Msg("failed to read config file")
		response.WriteHeader(http.StatusInternalServerError)
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to read configuration file"}) //nolint:errchkjson // simple encoding

		return
	}

	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"simtezilo-config-%s.json\"",
		time.Now().Format("20060102-150405")))

	_, err = response.Write(configData)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to write config data to response")
	}
}

// handleConfigImport handles POST requests to import and validate a configuration file.
func (h *configHandler) handleConfigImport(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")

	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Method not allowed"}) //nolint:errchkjson // simple encoding

		return
	}

	err := request.ParseMultipartForm(10 << 20)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to parse multipart form")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to parse form data"}) //nolint:errchkjson // simple encoding

		return
	}

	file, header, err := request.FormFile("config")
	if err != nil {
		h.log.Error().Err(err).Msg("failed to get file from form")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "No config file provided"}) //nolint:errchkjson // simple encoding

		return
	}
	defer file.Close()

	h.log.Info().Str("filename", header.Filename).Int64("size", header.Size).Msg("config import requested")

	fileContent, err := io.ReadAll(file)
	if err != nil {
		h.log.Error().Err(err).Msg("failed to read uploaded file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to read uploaded file"}) //nolint:errchkjson // simple encoding

		return
	}

	var testConfig map[string]any

	err = json.Unmarshal(fileContent, &testConfig)
	if err != nil {
		h.log.Error().Err(err).Msg("invalid JSON format")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": fmt.Sprintf("Invalid JSON format: %v", err)}) //nolint:errchkjson // simple encoding

		return
	}

	validationResult := h.config.ValidateConfig(fileContent)
	if !validationResult.Valid {
		h.log.Warn().Interface("errors", validationResult.Errors).Msg("config validation failed")
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"error":            "Configuration validation failed",
			"validationErrors": validationResult.Errors,
		})

		return
	}

	backupPath, err := h.config.BackupConfigFile()
	if err != nil {
		h.log.Warn().Err(err).Msg("failed to backup current config (continuing anyway)")
	} else {
		h.log.Info().Str("backup", backupPath).Msg("created config backup")
	}

	configFilePath := h.config.GetConfigFilePath()

	err = os.WriteFile(configFilePath, fileContent, 0o600)
	if err != nil {
		h.log.Error().Err(err).Str("file", configFilePath).Msg("failed to write config file")
		response.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(response).Encode(map[string]string{"error": "Failed to write configuration file"}) //nolint:errchkjson // simple encoding

		return
	}

	h.log.Info().Str("file", configFilePath).Msg("config file imported successfully")

	h.config.MarkRestartRequired()

	_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
		"status":  "success",
		"message": "Configuration imported successfully. Please restart the application for changes to take effect.",
		"backup":  backupPath,
	})
}

// handleCalibrationSweep handles POST requests to start/stop a calibration frequency sweep.
func (h *configHandler) handleCalibrationSweep(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	response.Header().Set("Content-Type", "application/json")

	var reqData struct {
		Action string `json:"action"`
	}

	err := json.NewDecoder(request.Body).Decode(&reqData)
	if err != nil {
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Invalid request body",
		})

		return
	}

	switch reqData.Action {
	case "start":
		h.calibrator.StartSweep() //nolint:contextcheck // sweep context is unrelated to request context
		h.log.Info().Msg("calibration sweep started")
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":    "success",
			"message":   "Sweep started",
			"sweeping":  true,
			"frequency": h.calibrator.GetSweepFrequency(),
		})
	case "stop":
		h.calibrator.StopSweep()
		h.log.Info().Msg("calibration sweep stopped")
		_ = json.NewEncoder(response).Encode(map[string]any{ //nolint:errchkjson // simple encoding
			"status":    "success",
			"message":   "Sweep stopped",
			"sweeping":  false,
			"frequency": h.calibrator.GetFrequency(),
		})
	default:
		response.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(response).Encode(map[string]string{ //nolint:errchkjson // simple encoding
			"status":  "error",
			"message": "Invalid action (must be 'start' or 'stop')",
		})
	}
}
