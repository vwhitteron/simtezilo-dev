package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kennygrant/sanitize"
	"github.com/vwhitteron/simtezilo-dev/app/codec"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
)

// recordingTriggerState tracks trigger toggles for start/stop of the recording.
type recordingTriggerState struct {
	lastTriggerState bool     // Previous state of the triggering input
	lastToggle       uint32   // Sequence ID of last toggle
	toggleCount      int      // Number of toggles in detection window
	toggleHistory    []uint32 // Sequence IDs of recent toggles
}

// recordingState tracks telemetry recording state.
type recordingState struct {
	startTime time.Time // When recording started
	filepath  string    // Current recording file path and name
	trigger   recordingTriggerState
}

// detectRecordingTrigger processes high beam state changes and triggers recording when toggled 3 times quickly.
func (a *App) detectRecordingTrigger() {
	// Ignore recording triggers for replays
	if !a.gtClient.Telemetry.Flags().Live {
		return
	}

	currentTriggerState := a.gtClient.Telemetry.Flags().HighBeamActive
	currentSequenceID := a.state.current.sequenceNumber

	// Check if the trigger state has changed
	if currentTriggerState == a.state.recorder.trigger.lastTriggerState {
		return
	}

	// Update the last state to current state
	a.state.recorder.trigger.lastTriggerState = currentTriggerState

	// Only count toggles when the trigger transitions to high/on/true (OFF->ON)
	if !currentTriggerState {
		return
	}

	a.state.recorder.trigger.lastToggle = currentSequenceID
	a.state.recorder.trigger.toggleHistory = append(a.state.recorder.trigger.toggleHistory, currentSequenceID)

	toggleWindowSeconds := uint32(1) // Remove toggles older than 2 seconds
	cutoffSequenceID := currentSequenceID - (toggleWindowSeconds * frameRate)
	validToggles := []uint32{}

	for _, toggleSequenceID := range a.state.recorder.trigger.toggleHistory {
		if toggleSequenceID > cutoffSequenceID {
			validToggles = append(validToggles, toggleSequenceID)
		}
	}

	a.state.recorder.trigger.toggleHistory = validToggles
	a.state.recorder.trigger.toggleCount = len(validToggles)

	a.log.Debug().
		Int("toggle_count", a.state.recorder.trigger.toggleCount).
		Uint32("sequence_id", currentSequenceID).
		Bool("high_beam_active", currentTriggerState).
		Msg("Recording trigger toggle detected")

	if a.state.recorder.trigger.toggleCount >= 3 {
		a.toggleRecording()
	}
}

// toggleRecording starts or stops telemetry recording.
func (a *App) toggleRecording() {
	if a.gtClient.IsRecording() {
		a.stopRecording()
	} else {
		a.startRecording()
	}
}

// startRecording begins telemetry capture.
func (a *App) startRecording() {
	if a.gtClient.IsRecording() {
		a.notifyRecordingEvent("error")

		a.log.Warn().Msg("Recording already in progress")

		return
	}

	timestamp := time.Now()
	a.state.recorder.startTime = timestamp

	filepath := filepath.Join(
		a.config.GetAppBaseDir(),
		"data",
		"replays",
		a.generateRecordingFilename(timestamp),
	)

	a.state.recorder.filepath = filepath

	err := a.gtClient.StartRecording(filepath)
	if err != nil {
		a.state.recorder.filepath = ""

		a.notifyRecordingEvent("error")

		a.log.Error().
			Err(err).
			Str("file", filepath).
			Msg("Start recording")

		return
	}

	a.notifyRecordingEvent("start")

	a.log.Info().
		Str("file", filepath).
		Msg("Start recording")
}

// stopRecording ends telemetry capture.
func (a *App) stopRecording() {
	a.state.recorder.trigger.toggleHistory = []uint32{}
	a.state.recorder.trigger.toggleCount = 0

	if !a.gtClient.IsRecording() {
		return
	}

	err := a.gtClient.StopRecording()
	if err != nil {
		a.notifyRecordingEvent("error")

		a.log.Error().
			Err(err).
			Str("file", a.state.recorder.filepath).
			Msg("Stop recording")
	}

	duration := time.Since(a.state.recorder.startTime)
	filepath := a.state.recorder.filepath

	a.state.recorder.filepath = ""

	a.notifyRecordingEvent("stop")

	a.log.Info().
		Str("file", filepath).
		Dur("duration", duration).
		Msg("Stop recording")
}

// notifyRecordingEvent sends a pitRadio notification for recording events.
func (a *App) notifyRecordingEvent(event string) {
	var sample string

	switch event {
	case "start":
		sample = "recordingStartTone"
	case "stop":
		sample = "recordingStopTone"
	case "error":
		sample = "errorTone"
	default:
		return
	}

	effectSample := a.synth.EffectSampleBank().GetSample(sample, codec.OpusSampleRate)

	dcaData, err := effectSample.ToDCA()
	if err != nil {
		a.log.Error().
			Err(err).
			Msg("generate talk permit tone")
	} else {
		err := a.pitRadio.Send(pitradio.Message{
			MessageType: pitradio.AudioMessage,
			Text:        "recording " + event,
			// Accent:      a.config.GetAppAccent(),
			Audio:   dcaData,
			NoCache: false,
		})
		if err != nil {
			a.log.Error().
				Err(err).
				Msg("send message")
		}
	}
}

// generateRecordingFilename creates a filename for the telemetry recording.
func (a *App) generateRecordingFilename(timestampe time.Time) string {
	trackName := a.sanitizeFilename(a.circuit.Name())
	if trackName == "" {
		trackName = "unknown_track"
	}

	vehicleManufacturer := a.sanitizeFilename(a.gtClient.Telemetry.VehicleManufacturer())
	if vehicleManufacturer == "" {
		vehicleManufacturer = "unknown_manufacturer"
	}

	vehicleModel := a.sanitizeFilename(a.gtClient.Telemetry.VehicleModel())
	if vehicleModel == "" {
		vehicleModel = "unknown_model"
	}

	// format: yyyymmdd.hhmmss-track_name-vehicle_manufacturer_vehicle_model.gtz
	filename := fmt.Sprintf("%s-%s-%s-%s.gtz",
		timestampe.Format("20060102.150405"),
		trackName,
		vehicleManufacturer,
		vehicleModel,
	)

	return filename
}

// sanitizeFilename cleans a string to be safe for use as a filename.
func (a *App) sanitizeFilename(input string) string {
	if input == "" {
		return input
	}

	result := sanitize.Name(input)
	result = strings.ReplaceAll(result, " ", "_")

	return result
}
