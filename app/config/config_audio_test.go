package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAudioSettersDoNotRequireRestart guards the registerUpdate(false) change:
// every audio setting that is applied live (device, name, channels, rate,
// latency, backend, and the pit-radio equivalents) must NOT raise the
// restart-required flag, otherwise the UI would nag for a restart after a change
// that already took effect.
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
		{"audio backend", func(c *Config) { c.SetAudioBackend("portaudio") }},
		{"pit radio device", func(c *Config) { c.SetAudioPitRadioDevice("2") }},
		{"pit radio device name", func(c *Config) { c.SetAudioPitRadioDeviceName("Headphones") }},
		{"pit radio sample rate", func(c *Config) { c.SetAudioPitRadioSampleRate(44100) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := newTestConfig()
			require.False(t, cfg.IsRestartRequired(), "fresh config should not require a restart")

			tc.apply(cfg)

			assert.False(t, cfg.IsRestartRequired(),
				"live-applied audio setting must not require a restart")
		})
	}
}

// TestNonLiveAudioSettingStillRequiresRestart is the contrast case: cushionMs is
// not applied live, so it should still raise the restart-required flag. It also
// ensures the flag mechanism itself works (so the test above is meaningful).
func TestNonLiveAudioSettingStillRequiresRestart(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()
	cfg.SetAudioHapticsCushionMs(40)

	assert.True(t, cfg.IsRestartRequired(),
		"cushionMs is not applied live and should require a restart")
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
