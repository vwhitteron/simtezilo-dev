package webui //nolint:testpackage // white-box: builds the unexported config handler directly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/calibrator"
	appconfig "github.com/vwhitteron/simtezilo-dev/app/config"
)

// newGetConfigTestHandler builds a configHandler with the collaborators
// handleGetConfig reads: a default config and a tone generator. The tone
// generator opens no audio device.
func newGetConfigTestHandler(t *testing.T) (*configHandler, *appconfig.Config) {
	t.Helper()

	cfg := appconfig.NewFromJSON([]byte("{}"), zerolog.Nop())

	cal, err := calibrator.NewToneGenerator(cfg)
	require.NoError(t, err)

	handler := &configHandler{
		log:                   zerolog.Nop(),
		config:                cfg,
		calibrator:            cal,
		deriveHapticsChannels: func() int { return 2 },
	}

	return handler, cfg
}

// getConfigSynthesizer performs a GET on the config API and returns the
// synthesizer section of the payload.
func getConfigSynthesizer(t *testing.T, handler *configHandler) map[string]any {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	recorder := httptest.NewRecorder()

	handler.handleGetConfig(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)

	var payload map[string]any

	err := json.Unmarshal(recorder.Body.Bytes(), &payload)
	require.NoError(t, err)

	synthesizer, ok := payload["synthesizer"].(map[string]any)
	require.True(t, ok, "payload has no synthesizer section")

	return synthesizer
}

// TestHandleGetConfig_EngineProfilesCarryEveryField guards the field names the
// settings page reads. A renamed JSON tag leaves the engine profile inputs blank.
func TestHandleGetConfig_EngineProfilesCarryEveryField(t *testing.T) {
	t.Parallel()

	// Arrange
	handler, _ := newGetConfigTestHandler(t)

	// Act
	synthesizer := getConfigSynthesizer(t, handler)

	// Assert
	engineProfiles, ok := synthesizer["engineProfiles"].(map[string]any)
	require.True(t, ok, "synthesizer has no engineProfiles map")
	assert.NotEmpty(t, engineProfiles)

	i4Profile, ok := engineProfiles["i4"].(map[string]any)
	require.True(t, ok, "engineProfiles has no i4 entry")
	assert.InDelta(t, 0.76, i4Profile["primaryBalance"], 0.001)
	assert.InDelta(t, 0.80, i4Profile["secondaryBalance"], 0.001)
	assert.InDelta(t, -4.25, i4Profile["gain"], 0.001)
	assert.InDelta(t, 0.75, i4Profile["pulseScale"], 0.001)
}

// TestHandleGetConfig_ReportsActiveEngineProfile verifies the settings page can
// select the profile the current vehicle resolved to.
func TestHandleGetConfig_ReportsActiveEngineProfile(t *testing.T) {
	t.Parallel()

	// Arrange
	handler, cfg := newGetConfigTestHandler(t)

	// Act & Assert - no vehicle resolved a profile yet
	synthesizer := getConfigSynthesizer(t, handler)
	assert.Empty(t, synthesizer["activeEngineProfile"])

	// Act - resolve a profile the way the engine generator does
	cfg.GetHapticsEngineProfile("i4")

	// Assert
	synthesizer = getConfigSynthesizer(t, handler)
	assert.Equal(t, "i4", synthesizer["activeEngineProfile"])
}
