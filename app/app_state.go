package app

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
	gtmodels "github.com/zetetos/gt-telemetry/pkg/models"
)

// raceState holds transient race data for haptic generation and pit radio notifications.
type raceState struct {
	// Telemetry session information
	sequenceNumber uint32        // Current telemetry sequence number
	sequenceDelta  uint32        // Delta between current and last telemetry sequence number
	timeOfDay      time.Duration // Time of day in the telemetry session

	// Vehicle information
	transmissionGear int     // Current transmission gear
	engineRPM        float32 // Current engine RPM

	// Race timing information
	lapNumber   int16         // Current lap number
	lastLapTime time.Duration // Last lap time duration
	isLive      bool          // Flag to indicate if the telemetry is live or a replay
	gameState   gtmodels.GameState
}

// gameState holds the overall application state.
type gameState struct {
	log                    zerolog.Logger
	hapticsEnabled         bool           // Flag to indicate if haptics are enabled // TODO: move state to haptics?
	telemetryActive        bool           // Flag to indicate if telemetry is active
	sessionEnded           bool           // Flag to indicate if session end has been handled
	raceCompleteTime       time.Time      // Real-world time when the race was completed
	current                raceState      // Race state at the current telemetry sequence
	last                   raceState      // Race state at the last telemetry sequence
	engine                 engineState    // Engine state for haptic generation
	recorder               recordingState // Telemetry recording state
	mainMenuFrameCount     int            // Counter for consecutive main menu frames
	isInPostRaceMenu       bool           // Flag to indicate if game is in post-race menu with fixed telemetry values
	postRaceMenuFrameCount int            // Counter for consecutive post-race menu static telemetry frames
}

// NewGameState creates and initializes a new appState instance.
func NewGameState(logger *zerolog.Logger) gameState { //nolint:revive // app is top-level so unexported gameState is fine
	return gameState{
		log: logger.With().Str("component", "appState").Logger(),
		current: raceState{
			transmissionGear: kinematics.NullGear,
		},
		last: raceState{
			transmissionGear: kinematics.NullGear,
		},
	}
}

// SetPostRaceMenuState updates the post-race menu state.
func (a *gameState) SetPostRaceMenuState(isInMenu bool) {
	if a.isInPostRaceMenu == isInMenu {
		return
	}

	a.isInPostRaceMenu = isInMenu

	a.log.Debug().Bool("isInPostRaceMenu", isInMenu).Msg("Post-race menu state changed")
}
