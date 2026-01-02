package app

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/kinematics"
)

// appState holds the overall application state.
type appState struct {
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

// NewAppState creates and initializes a new appState instance.
func NewAppState(logger *zerolog.Logger) appState {
	return appState{
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
func (a *appState) SetPostRaceMenuState(isInMenu bool) {
	if a.isInPostRaceMenu == isInMenu {
		return
	}

	a.isInPostRaceMenu = isInMenu

	a.log.Debug().Bool("isInPostRaceMenu", isInMenu).Msg("Post-race menu state changed")
}
