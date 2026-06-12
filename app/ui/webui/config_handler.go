package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
	"github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/updater"
)

type configHandler struct {
	log         zerolog.Logger
	config      *appconfig.Config
	calibrator  *calibrator.ToneGenerator
	updater     *updater.Updater
	broadcaster *Broadcaster
}

func newConfigHandler(log zerolog.Logger, config *appconfig.Config, cal *calibrator.ToneGenerator, upd *updater.Updater, b *Broadcaster) *configHandler {
	return &configHandler{
		log:         log,
		config:      config,
		calibrator:  cal,
		updater:     upd,
		broadcaster: b,
	}
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
			"language":       *h.config.GetAppLanguage(),
			"accent":         h.config.GetAppAccent(),
			"logLevel":       h.config.GetAppLogLevel(),
			"baseDir":        h.config.GetAppBaseDir(),
			"vehicleDBFile":  h.config.GetAppVehicleDBFile(),
			"enabledWebUI":   h.config.GetAppWebUIEnabled(),
			"webUIPort":      h.config.GetAppWebUIPort(),
			"enableDevTools": h.config.GetDevToolsEnabled(),
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
		"haptics": map[string]any{
			"enableReplay":                 h.config.GetHapticsReplayEnabled(),
			"pitRadioOutput":               h.config.GetPitRadioOutput(),
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
		},
		"pitRadio": map[string]any{
			"enabled":               h.config.PitRadioEnabled(),
			"messageSendIntervalMs": h.config.GetPitRadioMessageSendIntervalMs(),
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
			"internalSampleRateHz":      h.config.GetSynthInternalSampleRateHz(),
			"outputSampleRateHz":        h.config.GetSynthOutputSampleRateHz(),
			"outputFile":                h.config.GetSynthOutputFile(),
			"masterMute":                h.config.GetSynthMasterMute(),
			"masterGain":                h.config.GetSynthMasterGain(),
			"channel0Mute":              h.config.GetSynthChannelMute(0),
			"channel0Gain":              h.config.GetSynthChannelGain(0),
			"channel1Mute":              h.config.GetSynthChannelMute(1),
			"channel1Gain":              h.config.GetSynthChannelGain(1),
			"chassisMute":               h.config.GetSynthChassisMute(),
			"chassisGain":               h.config.GetSynthChassisGain(),
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
		},
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
		"telemetry": map[string]any{
			"source":    h.config.GetTelemetrySource(),
			"updateURL": h.config.GetTelemetryUpdateURL(),
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
	var errors []string

	for section, sectionData := range configData {
		sectionMap, ok := sectionData.(map[string]any)
		if !ok {
			errors = append(errors, "invalid data for section "+section)

			continue
		}

		switch section {
		case "app":
			errors = append(errors, h.applyAppConfig(sectionMap)...)
		case "synthesizer":
			errors = append(errors, h.applySynthesizerConfig(sectionMap)...)
		case "haptics":
			errors = append(errors, h.applyHapticsConfig(sectionMap)...)
		case "hardware":
			errors = append(errors, h.applyHardwareConfig(sectionMap)...)
		case "telemetry":
			errors = append(errors, h.applyTelemetryConfig(sectionMap)...)
		case "discord":
			errors = append(errors, h.applyDiscordConfig(sectionMap)...)
		case "calibration":
			errors = append(errors, h.applyCalibrationConfig(sectionMap)...)
		case "pitRadio":
			errors = append(errors, h.applyPitRadioConfig(sectionMap)...)
		default:
			h.log.Debug().Str("section", section).Msg("configuration section not implemented for updates")
		}
	}

	return errors
}

// applyAppConfig applies application configuration changes.
func (h *configHandler) applyAppConfig(config map[string]any) []string {
	var errors []string

	if language, ok := config["language"]; ok {
		if langStr, ok := language.(string); ok {
			h.config.SetAppLanguage(langStr)
		} else {
			errors = append(errors, "invalid language value")
		}
	}

	if accent, ok := config["accent"]; ok {
		if accentStr, ok := accent.(string); ok {
			h.config.SetAppAccent(accentStr)
		} else {
			errors = append(errors, "invalid accent value")
		}
	}

	if logLevel, ok := config["logLevel"]; ok {
		if levelStr, ok := logLevel.(string); ok {
			h.config.SetAppLogLevel(levelStr)
		} else {
			errors = append(errors, "invalid log level value")
		}
	}

	if baseDir, ok := config["baseDir"]; ok {
		if baseDirStr, ok := baseDir.(string); ok {
			h.config.SetAppBaseDir(baseDirStr)
		} else {
			errors = append(errors, "invalid base directory value")
		}
	}

	if enableDevTools, ok := config["enableDevTools"]; ok {
		if enableDevToolsBool, ok := enableDevTools.(bool); ok {
			h.config.SetDevToolsEnabled(enableDevToolsBool)
		} else {
			errors = append(errors, "invalid enableDevTools value")
		}
	}

	if updates, ok := config["updates"]; ok {
		if updatesMap, ok := updates.(map[string]any); ok {
			if autoCheck, ok := updatesMap["autoCheck"]; ok {
				if autoCheckBool, ok := autoCheck.(bool); ok {
					h.config.SetAppUpdateAutoCheck(autoCheckBool)
				} else {
					errors = append(errors, "invalid autoCheck value")
				}
			}

			if autoInstall, ok := updatesMap["autoInstall"]; ok {
				if autoInstallBool, ok := autoInstall.(bool); ok {
					h.config.SetAppUpdateAutoInstall(autoInstallBool)
				} else {
					errors = append(errors, "invalid autoInstall value")
				}
			}

			if checkIntervalMinutes, ok := updatesMap["checkIntervalMinutes"]; ok {
				if intervalFloat, ok := checkIntervalMinutes.(float64); ok {
					h.config.SetAppUpdateCheckIntervalMinutes(int(intervalFloat))
				} else {
					errors = append(errors, "invalid checkIntervalMinutes value")
				}
			}

			if channel, ok := updatesMap["channel"]; ok {
				if channelStr, ok := channel.(string); ok {
					h.config.SetAppUpdateChannel(channelStr)
					// TODO: this should probably be done outside of the config update function
					if h.updater != nil {
						h.updater.SetChannel(channelStr)
						h.updater.CheckExistingDownloads()
					}
				} else {
					errors = append(errors, "invalid channel value")
				}
			}
		} else {
			errors = append(errors, "invalid updates configuration")
		}
	}

	vehicleDBFile, fileOK := config["vehicleDBFile"]
	if !fileOK {
		return errors
	}

	vehicleDBFileStr, strOK := vehicleDBFile.(string)
	if !strOK {
		errors = append(errors, "invalid vehicle database file value")

		return errors
	}

	if vehicleDBFileStr != "" {
		_, err := os.Stat(vehicleDBFileStr)
		if err != nil {
			if os.IsNotExist(err) {
				errors = append(errors, "vehicle database file not found: "+vehicleDBFileStr)
			} else {
				errors = append(errors, "cannot access vehicle database file: "+err.Error())
			}

			return errors
		}
	}

	h.config.SetAppVehicleDBFile(vehicleDBFileStr)

	return errors
}

// applySynthesizerConfig applies synthesizer configuration changes.
func (h *configHandler) applySynthesizerConfig(config map[string]any) []string {
	var errors []string

	h.log.Debug().Interface("synthConfig", config).Msg("applying synthesizer configuration")

	if internalSampleRate, ok := config["internalSampleRateHz"]; ok {
		h.log.Debug().Interface("value", internalSampleRate).Type("type", internalSampleRate).Msg("processing internalSampleRateHz")

		if rateFloat, ok := internalSampleRate.(float64); ok {
			h.config.SetSynthInternalSampleRateHz(int(rateFloat))
			h.log.Debug().Int("rate", int(rateFloat)).Msg("set internal sample rate")
		} else {
			errors = append(errors, "invalid internal sample rate value")

			h.log.Error().Interface("value", internalSampleRate).Msg("invalid internal sample rate value type")
		}
	}

	if outputSampleRate, ok := config["outputSampleRateHz"]; ok {
		h.log.Debug().Interface("value", outputSampleRate).Type("type", outputSampleRate).Msg("processing outputSampleRateHz")

		if rateFloat, ok := outputSampleRate.(float64); ok {
			h.config.SetSynthOutputSampleRateHz(int(rateFloat))
			h.log.Debug().Int("rate", int(rateFloat)).Msg("set output sample rate")
		} else {
			errors = append(errors, "invalid output sample rate value")

			h.log.Error().Interface("value", outputSampleRate).Msg("invalid output sample rate value type")
		}
	}

	if masterGain, ok := config["masterGain"]; ok {
		if gainFloat, ok := masterGain.(float64); ok {
			h.config.SetSynthMasterGain(gainFloat)
		} else {
			errors = append(errors, "invalid master gain value")
		}
	}

	if masterMute, ok := config["masterMute"]; ok {
		if mute, ok := masterMute.(bool); ok {
			h.config.SetSynthMasterMute(mute)
		} else {
			errors = append(errors, "invalid master gain mute value")
		}
	}

	if channel0Gain, ok := config["channel0Gain"]; ok {
		if gainFloat, ok := channel0Gain.(float64); ok {
			h.config.SetSynthChannelGain(0, gainFloat)
		} else {
			errors = append(errors, "invalid left channel gain value")
		}
	}

	if channel0Mute, ok := config["channel0Mute"]; ok {
		if mute, ok := channel0Mute.(bool); ok {
			h.config.SetSynthChannelMute(0, mute)
		} else {
			errors = append(errors, "invalid left channel mute value")
		}
	}

	if channel1Gain, ok := config["channel1Gain"]; ok {
		if gainFloat, ok := channel1Gain.(float64); ok {
			h.config.SetSynthChannelGain(1, gainFloat)
		} else {
			errors = append(errors, "invalid right channel gain value")
		}
	}

	if channel1Mute, ok := config["channel1Mute"]; ok {
		if mute, ok := channel1Mute.(bool); ok {
			h.config.SetSynthChannelMute(1, mute)
		} else {
			errors = append(errors, "invalid right channel mute value")
		}
	}

	if chassisGain, ok := config["chassisGain"]; ok {
		if gainFloat, ok := chassisGain.(float64); ok {
			h.config.SetSynthChassisGain(gainFloat)
		} else {
			errors = append(errors, "invalid chassis gain value")
		}
	}

	if chassisMute, ok := config["chassisMute"]; ok {
		if mute, ok := chassisMute.(bool); ok {
			h.config.SetSynthChassisMute(mute)
		} else {
			errors = append(errors, "invalid chassis gain mute value")
		}
	}

	if transmissionGain, ok := config["transmissionGain"]; ok {
		if gainFloat, ok := transmissionGain.(float64); ok {
			h.config.SetSynthTransmissionGain(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain value")
		}
	}

	if transmissionMute, ok := config["transmissionMute"]; ok {
		if mute, ok := transmissionMute.(bool); ok {
			h.config.SetSynthTransmissionMute(mute)
		} else {
			errors = append(errors, "invalid transmission gain mute value")
		}
	}

	if transmissionGainMinRace, ok := config["transmissionGainMinRace"]; ok {
		if gainFloat, ok := transmissionGainMinRace.(float64); ok {
			h.config.SetSynthTransmissionGainMinRace(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain min race value")
		}
	}

	if transmissionGainMinStreet, ok := config["transmissionGainMinStreet"]; ok {
		if gainFloat, ok := transmissionGainMinStreet.(float64); ok {
			h.config.SetSynthTransmissionGainMinStreet(gainFloat)
		} else {
			errors = append(errors, "invalid transmission gain min street value")
		}
	}

	if engineGain, ok := config["engineGain"]; ok {
		if gainFloat, ok := engineGain.(float64); ok {
			h.config.SetSynthEngineGain(gainFloat)
		} else {
			errors = append(errors, "invalid engine gain value")
		}
	}

	if engineMute, ok := config["engineMute"]; ok {
		if mute, ok := engineMute.(bool); ok {
			h.config.SetSynthEngineMute(mute)
		} else {
			errors = append(errors, "invalid engine gain mute value")
		}
	}

	if gainIncrement, ok := config["gainIncrement"]; ok {
		if incrementFloat, ok := gainIncrement.(float64); ok {
			h.config.SetSynthGainIncrement(incrementFloat)
		} else {
			errors = append(errors, "invalid gain increment value")
		}
	}

	if engineProfiles, ok := config["engineProfiles"]; ok {
		if profilesMap, ok := engineProfiles.(map[string]any); ok {
			for name, profileData := range profilesMap {
				if profileMap, ok := profileData.(map[string]any); ok {
					profile := haptics.EngineProfile{}

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
		} else {
			errors = append(errors, "invalid engine profiles format")
		}
	}

	if eqEnabled, ok := config["enableEQ"]; ok {
		if enabledArray, ok := eqEnabled.([]any); ok {
			for channel, val := range enabledArray {
				if enabled, ok := val.(bool); ok {
					h.config.SetSynthChannelEqEnabled(channel, enabled)
				} else {
					errors = append(errors, fmt.Sprintf("invalid EQ enabled value for channel %d", channel))
				}
			}
		} else {
			errors = append(errors, "invalid EQ enabled value (expected array)")
		}
	}

	if drxEnabled, ok := config["enableDrx"]; ok {
		if enabled, ok := drxEnabled.(bool); ok {
			h.config.SetSynthDRXEnabled(enabled)
		} else {
			errors = append(errors, "invalid DRX enabled value (expected bool)")
		}
	}

	if eq, ok := config["eq"]; ok {
		if channelArray, ok := eq.([]any); ok {
			for channel, channelVal := range channelArray {
				if eqArray, ok := channelVal.([]any); ok {
					eqBands := make([]appconfig.EQBand, 0, len(eqArray))
					for idx, val := range eqArray {
						if bandMap, ok := val.(map[string]any); ok {
							freq, freqOk := bandMap["frequency"].(float64)
							gain, gainOk := bandMap["gain"].(float64)
							qVal, qOk := bandMap["q"].(float64)

							if !freqOk || !gainOk || !qOk {
								errors = append(errors, fmt.Sprintf("invalid EQ band %d for channel %d: missing or invalid fields", idx+1, channel))

								continue
							}

							eqBands = append(eqBands, appconfig.EQBand{
								Frequency: freq,
								Gain:      gain,
								Q:         qVal,
							})
						} else {
							errors = append(errors, fmt.Sprintf("invalid EQ band %d format for channel %d", idx+1, channel))

							continue
						}
					}

					if len(eqBands) == 8 {
						h.config.SetSynthChannelEq(channel, eqBands)
					} else {
						errors = append(errors, fmt.Sprintf("EQ for channel %d must have exactly 8 bands, got %d", channel, len(eqBands)))
					}
				} else {
					errors = append(errors, fmt.Sprintf("invalid EQ format for channel %d", channel))
				}
			}
		} else {
			errors = append(errors, "invalid EQ format (expected array of channel arrays)")
		}
	}

	return errors
}

// applyHapticsConfig applies haptics configuration changes.
//
//nolint:cyclop // function is easy to understand
func (h *configHandler) applyHapticsConfig(config map[string]any) []string {
	var errors []string

	parseFloat := func(val any, key string) (float64, bool) {
		f, ok := val.(float64)
		if !ok {
			errors = append(errors, "invalid "+key+" value")
		}

		return f, ok
	}

	parseBool := func(val any, key string) (bool, bool) {
		b, ok := val.(bool)
		if !ok {
			errors = append(errors, "invalid "+key+" value")
		}

		return b, ok
	}

	if dynamicTransmission, ok := config["dynamicTransmissionFeedback"]; ok {
		if dynamicBool, ok := parseBool(dynamicTransmission, "dynamic transmission feedback"); ok {
			h.config.SetHapticsDynamicTransFeedbackEnabled(dynamicBool)
		}
	}

	if jerkCurve, ok := config["jerkCurve"]; ok {
		if curveFloat, ok := parseFloat(jerkCurve, "jerk curve"); ok {
			h.config.SetHapticsJerkCurve(int(curveFloat * 1000.0))
		}
	}

	if jerkMax, ok := config["jerkMax"]; ok {
		if maxFloat, ok := parseFloat(jerkMax, "jerk max"); ok {
			h.config.SetHapticsJerkMax(int(maxFloat))
		}
	}

	if snapCurve, ok := config["snapCurve"]; ok {
		if curveFloat, ok := parseFloat(snapCurve, "snap curve"); ok {
			h.config.SetHapticsSnapCurve(int(curveFloat * 1000.0))
		}
	}

	if snapMax, ok := config["snapMax"]; ok {
		if maxFloat, ok := parseFloat(snapMax, "snap max"); ok {
			h.config.SetHapticsSnapMax(int(maxFloat))
		}
	}

	if transmissionCurve, ok := config["dynamicTransmissionCurve"]; ok {
		if curveFloat, ok := parseFloat(transmissionCurve, "transmission curve"); ok {
			h.config.SetHapticsTransmissionCurve(int(curveFloat * 1000.0))
		}
	}

	if transmissionGforceMax, ok := config["dynamicTransmissionGforceMax"]; ok {
		if gforceFloat, ok := parseFloat(transmissionGforceMax, "transmission G-force max"); ok {
			h.config.SetHapticsTransmissionGforceMax(gforceFloat)
		}
	}

	if pulseMaxAmplitude, ok := config["pulseMaxAmplitude"]; ok {
		if amplitudeFloat, ok := parseFloat(pulseMaxAmplitude, "pulse max amplitude"); ok {
			h.config.SetHapticsPulseMaxAmplitude(amplitudeFloat)
		}
	}

	if pulseMaxFreq, ok := config["pulseMaxFrequencyHz"]; ok {
		if freqFloat, ok := parseFloat(pulseMaxFreq, "pulse max frequency"); ok {
			h.config.SetHapticsPulseMaxFrequencyHz(freqFloat)
		}
	}

	if pulseMinFreq, ok := config["pulseMinFrequencyHz"]; ok {
		if freqFloat, ok := parseFloat(pulseMinFreq, "pulse min frequency"); ok {
			h.config.SetHapticsPulseMinFrequencyHz(freqFloat)
		}
	}

	if enableReplay, ok := config["enableReplay"]; ok {
		if replayBool, ok := parseBool(enableReplay, "enable replay"); ok {
			h.config.SetHapticsEnableReplay(replayBool)
		}
	}

	if pitRadioOutput, ok := config["pitRadioOutput"]; ok {
		if outputStr, ok := pitRadioOutput.(string); ok {
			h.config.SetPitRadioOutput(outputStr)
		} else {
			errors = append(errors, "invalid pit radio output value")
		}
	}

	return errors
}

// applyCalibrationConfig applies calibration configuration changes.
func (h *configHandler) applyCalibrationConfig(config map[string]any) []string {
	var errors []string

	if enabled, ok := config["enabled"]; ok {
		if enabledBool, ok := enabled.(bool); ok {
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
		} else {
			errors = append(errors, "invalid calibration enabled value")
		}
	}

	if frequency, ok := config["frequency"]; ok {
		if freqFloat, ok := frequency.(float64); ok {
			h.calibrator.SetFrequency(freqFloat)
		} else {
			errors = append(errors, "invalid calibration frequency value")
		}
	}

	if sweepMin, ok := config["sweepMin"]; ok {
		if minFloat, ok := sweepMin.(float64); ok {
			h.calibrator.SetSweepMin(minFloat)
		} else {
			errors = append(errors, "invalid calibration sweepMin value")
		}
	}

	if sweepMax, ok := config["sweepMax"]; ok {
		if maxFloat, ok := sweepMax.(float64); ok {
			h.calibrator.SetSweepMax(maxFloat)
		} else {
			errors = append(errors, "invalid calibration sweepMax value")
		}
	}

	if sweepDuration, ok := config["sweepDuration"]; ok {
		if durationFloat, ok := sweepDuration.(float64); ok {
			h.calibrator.SetSweepDuration(durationFloat)
		} else {
			errors = append(errors, "invalid calibration sweepDuration value")
		}
	}

	return errors
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

	if monitoringEnabled, ok := config["enabled"]; ok {
		if enabledBool, ok := monitoringEnabled.(bool); ok {
			h.config.SetPitRadioFuelMonitoringEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid fuel monitoring enabled value")
		}
	}

	if preWarnLaps, ok := config["preWarnNotifyLaps"]; ok {
		if lapsFloat, ok := preWarnLaps.(float64); ok {
			h.config.SetPitRadioFuelPreWarnNotifyLaps(lapsFloat)
		} else {
			errors = append(errors, "invalid pre-warn notify laps value")
		}
	}

	if strategyLaps, ok := config["strategyNotifyLaps"]; ok {
		if lapsFloat, ok := strategyLaps.(float64); ok {
			h.config.SetPitRadioFuelStrategyNotifyLaps(lapsFloat)
		} else {
			errors = append(errors, "invalid strategy notify laps value")
		}
	}

	if safetyMarginLaps, ok := config["rangeSafetyMarginLaps"]; ok {
		if marginFloat, ok := safetyMarginLaps.(float64); ok {
			h.config.SetPitRadioFuelRangeSafetyMarginLaps(marginFloat)
		} else {
			errors = append(errors, "invalid range safety margin laps value")
		}
	}

	if safetyMarginMetres, ok := config["rangeSafetyMarginMetres"]; ok {
		if marginFloat, ok := safetyMarginMetres.(float64); ok {
			h.config.SetPitRadioFuelRangeSafetyMarginMetres(marginFloat)
		} else {
			errors = append(errors, "invalid range safety margin metres value")
		}
	}

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

	if raceProgressEnabled, ok := config["enableRaceProgress"]; ok {
		if enabledBool, ok := raceProgressEnabled.(bool); ok {
			h.config.SetPitRadioNotifyRaceProgressEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid race progress enabled value")
		}
	}

	if raceProgressMinLaps, ok := config["raceProgressMinLaps"]; ok {
		if lapsFloat, ok := raceProgressMinLaps.(float64); ok {
			h.config.SetPitRadioNotifyRaceProgressMinLaps(int(lapsFloat))
		} else {
			errors = append(errors, "invalid race progress min laps value")
		}
	}

	if raceProgressIntervalPc, ok := config["raceProgressIntervalPc"]; ok {
		if intervalFloat, ok := raceProgressIntervalPc.(float64); ok {
			h.config.SetPitRadioNotifyRaceProgressIntervalPc(int(intervalFloat))
		} else {
			errors = append(errors, "invalid race progress interval percentage value")
		}
	}

	if raceLapsEnabled, ok := config["enableRaceLaps"]; ok {
		if enabledBool, ok := raceLapsEnabled.(bool); ok {
			h.config.SetPitRadioNotifyRaceLapsEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid race laps enabled value")
		}
	}

	if raceLapsIntervalLaps, ok := config["raceLapsIntervalLaps"]; ok {
		if intervalFloat, ok := raceLapsIntervalLaps.(float64); ok {
			h.config.SetPitRadioNotifyRaceLapsIntervalLaps(int(intervalFloat))
		} else {
			errors = append(errors, "invalid race laps interval laps value")
		}
	}

	if raceLapsCountdownLaps, ok := config["raceLapsCountdownLaps"]; ok {
		if countdownFloat, ok := raceLapsCountdownLaps.(float64); ok {
			h.config.SetPitRadioNotifyRaceLapsCountdownLaps(int(countdownFloat))
		} else {
			errors = append(errors, "invalid race laps countdown laps value")
		}
	}

	if lapTimesEnabled, ok := config["enableLapTimes"]; ok {
		if enabledBool, ok := lapTimesEnabled.(bool); ok {
			h.config.SetPitRadioNotifyLapTimesEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid lap times enabled value")
		}
	}

	if lapTimesMaxDelta, ok := config["lapTimesMaxDeltaSeconds"]; ok {
		if deltaFloat, ok := lapTimesMaxDelta.(float64); ok {
			h.config.SetPitRadioNotifyLapTimesMaxDeltaSeconds(deltaFloat)
		} else {
			errors = append(errors, "invalid lap times max delta seconds value")
		}
	}

	if circuitMatchingEnabled, ok := config["enableCircuitMatching"]; ok {
		if enabledBool, ok := circuitMatchingEnabled.(bool); ok {
			h.config.SetPitRadioNotifyCircuitMatchingEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid circuit matching enabled value")
		}
	}

	return errors
}

// applyPitRadioConfig applies pit radio configuration changes.
func (h *configHandler) applyPitRadioConfig(config map[string]any) []string {
	var errors []string

	if enabled, ok := config["enabled"]; ok {
		if enabledBool, ok := enabled.(bool); ok {
			h.config.SetPitRadioEnabled(enabledBool)
		} else {
			errors = append(errors, "invalid pit radio enabled value")
		}
	}

	if intervalMs, ok := config["messageSendIntervalMs"]; ok {
		if intervalFloat, ok := intervalMs.(float64); ok {
			h.config.SetPitRadioMessageSendIntervalMs(int(intervalFloat))
		} else {
			errors = append(errors, "invalid message send interval value")
		}
	}

	if notificationsConfig, ok := config["notifications"]; ok {
		if notificationsMap, ok := notificationsConfig.(map[string]any); ok {
			notificationsErrors := h.applyNotificationsConfig(notificationsMap)
			errors = append(errors, notificationsErrors...)
		} else {
			errors = append(errors, "invalid notifications configuration structure")
		}
	}

	if discordConfig, ok := config["discord"]; ok {
		if discordMap, ok := discordConfig.(map[string]any); ok {
			discordErrors := h.applyDiscordConfig(discordMap)
			errors = append(errors, discordErrors...)
		} else {
			errors = append(errors, "invalid discord configuration structure")
		}
	}

	if fuelMonitoringConfig, ok := config["fuelMonitoring"]; ok {
		if fuelMap, ok := fuelMonitoringConfig.(map[string]any); ok {
			fuelErrors := h.applyFuelConfig(fuelMap)
			errors = append(errors, fuelErrors...)
		} else {
			errors = append(errors, "invalid fuel monitoring configuration structure")
		}
	}

	if tyreMonitoringConfig, ok := config["tyreMonitoring"]; ok {
		if tyreMap, ok := tyreMonitoringConfig.(map[string]any); ok {
			tyreErrors := h.applyTyresConfig(tyreMap)
			errors = append(errors, tyreErrors...)
		} else {
			errors = append(errors, "invalid tyre monitoring configuration structure")
		}
	}

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
