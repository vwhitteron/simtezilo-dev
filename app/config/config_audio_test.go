package config //nolint:testpackage // white-box: uses newTestConfig from config_test.go

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAudioSettersDoNotRequireRestart guards the registerUpdate(false) change:
// every audio setting that is applied live (device, name, channels, rate,
// latency, and the pit-radio equivalents) must NOT raise the restart-required
// flag, otherwise the UI would nag for a restart after a change that already
// took effect. The backend selection is the exception (see
// TestRestartRequiredAudioSettings).
func TestAudioSettersDoNotRequireRestart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		apply func(*Config)
	}{
		{"haptics device", func(c *Config) { c.SetAudioHapticsDevice("5") }},
		{"haptics device name", func(c *Config) { c.SetAudioHapticsDeviceName("Speakers") }},
		{"haptics channels", func(c *Config) { c.SetAudioHapticsChannels(4) }},
		{"haptics sample rate", func(c *Config) { c.SetAudioHapticsSampleRate(48000) }},
		{"haptics latency", func(c *Config) { c.SetAudioHapticsLatencyMs(40) }},
		{"pit radio device", func(c *Config) { c.SetAudioPitRadioDevice("2") }},
		{"pit radio device name", func(c *Config) { c.SetAudioPitRadioDeviceName("Headphones") }},
		{"pit radio sample rate", func(c *Config) { c.SetAudioPitRadioSampleRate(44100) }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := newTestConfig()
			require.False(t, cfg.IsRestartRequired(), "fresh config should not require a restart")

			testCase.apply(cfg)

			assert.False(t, cfg.IsRestartRequired(),
				"live-applied audio setting must not require a restart")
		})
	}
}

// TestRestartRequiredAudioSettings is the contrast case: the backend selection
// changes the whole audio stack and cushionMs is not applied live, so both must
// raise the restart-required flag. It also ensures the flag mechanism itself
// works (so the live-setter test above is meaningful).
func TestRestartRequiredAudioSettings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		apply func(*Config)
	}{
		{"audio backend", func(c *Config) { c.SetAudioBackend("portaudio") }},
		{"haptics cushion", func(c *Config) { c.SetAudioHapticsCushionMs(40) }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := newTestConfig()
			require.False(t, cfg.IsRestartRequired(), "fresh config should not require a restart")

			testCase.apply(cfg)

			assert.True(t, cfg.IsRestartRequired(),
				"setting is not applied live and should require a restart")
		})
	}
}

// TestAudioDeviceNameRoundTrip covers the new deviceName fields.
func TestAudioDeviceNameRoundTrip(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()

	cfg.SetAudioHapticsDeviceName("Speakers")
	assert.Equal(t, "Speakers", cfg.GetAudioHapticsDeviceName())

	cfg.SetAudioPitRadioDeviceName("Headphones")
	assert.Equal(t, "Headphones", cfg.GetAudioPitRadioDeviceName())
}
