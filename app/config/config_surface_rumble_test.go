package config //nolint:testpackage // white-box testing for internal config methods

import (
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// surfaceRumbleTolerance is the largest gap tolerated between a migrated setting
// and the internal value it was derived from, which covers the two-decimal rounding
// of the coarseness setting.
const surfaceRumbleTolerance = 1e-3

// testSurfaceRumbleMigrateInverted checks that a 1.1.0 config's tuned surface is
// converted to the 0-100 scale and a surface it omits is converted from its
// pre-1.2.0 default.
func testSurfaceRumbleMigrateInverted(t *testing.T) {
	t.Parallel()

	// Arrange
	jsonData := []byte(`{
		"schemaVersion": "1.1.0",
		"haptics": {"surfaceRumble": {"tarmac": {"level": 0.40, "coarseness": 1.00}}}
	}`)

	// Act
	cfg := NewFromJSON(jsonData, zerolog.Nop())

	// Assert
	tarmac, found := cfg.GetHapticsSurfaceRumble("tarmac")
	require.True(t, found)
	assert.InDelta(t, 20.00, tarmac.Level, 1e-9)
	assert.InDelta(t, 52.63, tarmac.Coarseness, 1e-9)

	dirt, found := cfg.GetHapticsSurfaceRumble("dirt")
	require.True(t, found)
	assert.InDelta(t, 42.50, dirt.Level, 1e-9)
	assert.InDelta(t, 63.16, dirt.Coarseness, 1e-9)

	cfg.mu.RLock()
	defer cfg.mu.RUnlock()

	assert.Equal(t, currentSchemaVersion, cfg.viper.SchemaVersion)
}

// testSurfaceRumbleMigrateLegacy checks that a 1.0.0 config passes through both
// migrations and ends at the current version with its rumble converted.
func testSurfaceRumbleMigrateLegacy(t *testing.T) {
	t.Parallel()

	// Arrange
	jsonData := []byte(`{
		"schemaVersion": "1.0.0",
		"haptics": {"surfaceRumble": {"grass": {"level": 0.65, "coarseness": 0.85}, "sand": {"level": 2, "coarseness": 0.1}}}
	}`)

	// Act
	cfg := NewFromJSON(jsonData, zerolog.Nop())

	// Assert
	grass, found := cfg.GetHapticsSurfaceRumble("grass")
	require.True(t, found)
	assert.InDelta(t, 32.50, grass.Level, 1e-9)
	assert.InDelta(t, 60.53, grass.Coarseness, 1e-9)

	sand, found := cfg.GetHapticsSurfaceRumble("sand")
	require.True(t, found)
	assert.InDelta(t, 100.0, sand.Level, 1e-9)
	assert.InDelta(t, 100.0, sand.Coarseness, 1e-9)

	snow, found := cfg.GetHapticsSurfaceRumble("snow")
	require.True(t, found)
	assert.InDelta(t, 30.00, snow.Level, 1e-9)
	assert.InDelta(t, 57.89, snow.Coarseness, 1e-9)

	cfg.mu.RLock()
	defer cfg.mu.RUnlock()

	assert.Equal(t, currentSchemaVersion, cfg.viper.SchemaVersion)
}

// testSurfaceRumbleMigrateCurrent checks that a 1.2.0 config is left untouched.
func testSurfaceRumbleMigrateCurrent(t *testing.T) {
	t.Parallel()

	// Arrange
	jsonData := []byte(`{
		"schemaVersion": "1.2.0",
		"haptics": {"surfaceRumble": {"tarmac": {"level": 0.40, "coarseness": 1.00}}}
	}`)

	// Act
	cfg := NewFromJSON(jsonData, zerolog.Nop())

	// Assert
	tarmac, found := cfg.GetHapticsSurfaceRumble("tarmac")
	require.True(t, found)
	assert.InDelta(t, 0.40, tarmac.Level, 1e-9)
	assert.InDelta(t, 1.00, tarmac.Coarseness, 1e-9)

	wantGrass := defaultConfig().Haptics.SurfaceRumble["grass"]
	grass, found := cfg.GetHapticsSurfaceRumble("grass")
	require.True(t, found)
	assert.InDelta(t, wantGrass.Level, grass.Level, 1e-9)
	assert.InDelta(t, wantGrass.Coarseness, grass.Coarseness, 1e-9)
}

// testSurfaceRumbleUpgradeJSON checks that a 1.1.0 import is upgraded before it is
// validated, and that a version-less file is returned unchanged.
func testSurfaceRumbleUpgradeJSON(t *testing.T) {
	t.Parallel()

	// Arrange
	invertedJSON := []byte(`{"schemaVersion": "1.1.0", "haptics": {"surfaceRumble": {"tarmac": {"level": 0.40, "coarseness": 1.00}}}}`)
	versionless := []byte(`{"haptics": {}}`)

	// Act
	upgraded, upgradeErr := upgradeLegacyJSON(invertedJSON)
	unchanged, unchangedErr := upgradeLegacyJSON(versionless)

	// Assert
	require.NoError(t, upgradeErr)

	var got viperConfig

	require.NoError(t, json.Unmarshal(upgraded, &got))
	assert.Equal(t, currentSchemaVersion, got.SchemaVersion)
	assert.InDelta(t, 20.00, got.Haptics.SurfaceRumble["tarmac"].Level, 1e-9)
	assert.InDelta(t, 52.63, got.Haptics.SurfaceRumble["tarmac"].Coarseness, 1e-9)
	assert.InDelta(t, 15.00, got.Haptics.SurfaceRumble["sand"].Level, 1e-9)

	require.NoError(t, unchangedErr)
	assert.JSONEq(t, string(versionless), string(unchanged))
}

// testSurfaceRumbleMigrateRoundTrip checks that every pre-1.2.0 default, once
// migrated, converts back to the internal value it held.
func testSurfaceRumbleMigrateRoundTrip(t *testing.T) {
	t.Parallel()

	// Arrange
	jsonData := []byte(`{"schemaVersion": "1.1.0"}`)
	legacy := legacyDefaultSurfaceRumble()

	// Act
	cfg := NewFromJSON(jsonData, zerolog.Nop())

	// Assert
	for surface, want := range legacy {
		got, found := cfg.GetHapticsSurfaceRumble(surface)
		require.True(t, found, surface)
		assert.InDelta(t, want.Level, got.Gain(), surfaceRumbleTolerance, surface)
		assert.InDelta(t, want.Coarseness, got.CutoffScale(), surfaceRumbleTolerance, surface)
	}
}

// testSurfaceRumbleSetClamps checks that SetHapticsSurfaceRumble clamps both
// settings to 0..100 and rounds them to two decimal places.
func testSurfaceRumbleSetClamps(t *testing.T) {
	t.Parallel()

	// Arrange
	cfg := NewFromJSON([]byte(`{"schemaVersion": "1.2.0"}`), zerolog.Nop())

	// Act
	cfg.SetHapticsSurfaceRumble("tarmac", SurfaceRumble{Level: 150, Coarseness: -5})
	high, _ := cfg.GetHapticsSurfaceRumble("tarmac")

	cfg.SetHapticsSurfaceRumble("tarmac", SurfaceRumble{Level: -1, Coarseness: 250})
	low, _ := cfg.GetHapticsSurfaceRumble("tarmac")

	cfg.SetHapticsSurfaceRumble("tarmac", SurfaceRumble{Level: 12.3456, Coarseness: 78.9012})
	rounded, _ := cfg.GetHapticsSurfaceRumble("tarmac")

	// Assert
	assert.InDelta(t, 100.0, high.Level, 1e-9)
	assert.InDelta(t, 0.0, high.Coarseness, 1e-9)
	assert.InDelta(t, 0.0, low.Level, 1e-9)
	assert.InDelta(t, 100.0, low.Coarseness, 1e-9)
	assert.InDelta(t, 12.35, rounded.Level, 1e-9)
	assert.InDelta(t, 78.90, rounded.Coarseness, 1e-9)
}

func TestSurfaceRumble(t *testing.T) {
	t.Parallel()

	t.Run("testSurfaceRumbleMigrateInverted", testSurfaceRumbleMigrateInverted)
	t.Run("testSurfaceRumbleMigrateLegacy", testSurfaceRumbleMigrateLegacy)
	t.Run("testSurfaceRumbleMigrateCurrent", testSurfaceRumbleMigrateCurrent)
	t.Run("testSurfaceRumbleUpgradeJSON", testSurfaceRumbleUpgradeJSON)
	t.Run("testSurfaceRumbleMigrateRoundTrip", testSurfaceRumbleMigrateRoundTrip)
	t.Run("testSurfaceRumbleSetClamps", testSurfaceRumbleSetClamps)
}
