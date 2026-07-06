package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSynthRoutingDefaults verifies the default routing matrix has all three
// sources enabled on every channel, preserving the historical broadcast/1:1
// behaviour.
func TestSynthRoutingDefaults(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()

	routing := cfg.GetSynthRouting()

	for _, source := range []string{RoutingSourceEngine, RoutingSourceChassis, RoutingSourceTransmission} {
		row, ok := routing[source]
		require.Truef(t, ok, "missing routing row for %q", source)
		require.Len(t, row, 2, "default routing row %q should match default channel count", source)

		for ch, enabled := range row {
			assert.Truef(t, enabled, "default routing %q ch%d should be enabled", source, ch)
		}
	}
}

// TestSynthRoutingMigration verifies a config lacking a routing block is filled
// with enabled defaults during finalisation.
func TestSynthRoutingMigration(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()

	// Simulate a legacy config with no routing block.
	cfg.mu.Lock()
	cfg.viper.Synthesizer.Routing = nil
	cfg.mu.Unlock()

	cfg.finalise()
	cfg.rebuildSnapshot()

	routing := cfg.GetSynthRouting()
	require.Len(t, routing, 3)

	for _, source := range routingSources {
		row, ok := routing[source]
		require.Truef(t, ok, "migration should add routing row for %q", source)
		require.Len(t, row, 2)
		assert.True(t, row[0] && row[1], "migrated routing %q should default to enabled", source)
	}
}

// TestSynthRoutingResize verifies routing rows grow to match an increased channel
// count, with newly added cells defaulting to enabled and existing cells preserved.
func TestSynthRoutingResize(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()

	// Disable transmission on channel 1 so we can confirm it survives the resize.
	cfg.SetSynthRoute(RoutingSourceTransmission, 1, false)
	require.False(t, cfg.GetSynthRouteEnabled(RoutingSourceTransmission, 1))

	// Grow to four output channels and re-finalise.
	cfg.mu.Lock()
	cfg.viper.Haptics.Output.Channels = 4
	cfg.mu.Unlock()

	cfg.finalise()
	cfg.rebuildSnapshot()

	routing := cfg.GetSynthRouting()
	for _, source := range routingSources {
		require.Lenf(t, routing[source], 4, "row %q should resize to 4 channels", source)
	}

	// Preserved existing cell.
	assert.False(t, cfg.GetSynthRouteEnabled(RoutingSourceTransmission, 1))
	// Newly added cells default to enabled.
	assert.True(t, cfg.GetSynthRouteEnabled(RoutingSourceTransmission, 2))
	assert.True(t, cfg.GetSynthRouteEnabled(RoutingSourceTransmission, 3))
}

// TestSynthRoutingShrink verifies routing rows shrink when the channel count is
// reduced.
func TestSynthRoutingShrink(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()

	cfg.mu.Lock()
	cfg.viper.Haptics.Output.Channels = 1
	cfg.mu.Unlock()

	cfg.finalise()
	cfg.rebuildSnapshot()

	for _, source := range routingSources {
		require.Lenf(t, cfg.GetSynthRouting()[source], 1, "row %q should shrink to 1 channel", source)
	}

	// Out-of-range reads are safe.
	assert.False(t, cfg.GetSynthRouteEnabled(RoutingSourceEngine, 1))
}

// TestSynthRoutingSetGet verifies the set/get round trip and out-of-range safety.
func TestSynthRoutingSetGet(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()

	cfg.SetSynthRoute(RoutingSourceEngine, 0, false)
	assert.False(t, cfg.GetSynthRouteEnabled(RoutingSourceEngine, 0))
	assert.True(t, cfg.GetSynthRouteEnabled(RoutingSourceEngine, 1))

	cfg.SetSynthRoute(RoutingSourceEngine, 0, true)
	assert.True(t, cfg.GetSynthRouteEnabled(RoutingSourceEngine, 0))

	// Out-of-range and unknown-source operations are no-ops, not panics.
	cfg.SetSynthRoute(RoutingSourceEngine, 99, false)
	cfg.SetSynthRoute("bogus", 0, true)
	assert.False(t, cfg.GetSynthRouteEnabled("bogus", 0))
}

// TestSynthRoutingPrunesUnknownSources verifies finalise drops rows for sources
// that are not recognised.
func TestSynthRoutingPrunesUnknownSources(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()

	cfg.mu.Lock()
	cfg.viper.Synthesizer.Routing["bogus"] = []bool{true, true}
	cfg.mu.Unlock()

	cfg.finalise()
	cfg.rebuildSnapshot()

	_, ok := cfg.GetSynthRouting()["bogus"]
	assert.False(t, ok, "unknown source row should be pruned")
}
