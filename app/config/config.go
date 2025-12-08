// Package config provides configuration management for the application.
package config

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	appHaptics "github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
)

type app struct {
	Language     string `toml:"language"`
	Accent       string `toml:"accent"`
	LogLevel     string `toml:"logLevel"`
	DataDir      string `toml:"dataDir"`
	ReplayMode   bool   `toml:"replayMode"`
	WebUIEnabled bool   `toml:"webUIEnabled"`
	WebUIPort    int    `toml:"webUIPort"`
}

type discord struct {
	Enabled        bool   `toml:"enabled"`
	Token          string `toml:"token"`
	GuildID        string `toml:"guildID"`
	ChannelID      string `toml:"channelID"`
	VoiceChannelID string `toml:"voiceChannelID"`
}

type fuel struct {
	MonitoringEnabled       bool    `toml:"monitoringEnabled"`
	PreWarnNotifyLaps       float64 `toml:"preWarnNotifyLaps"`
	StrategyNotifyLaps      float64 `toml:"strategyNotifyLaps"`
	RangeSafetyMarginLaps   float64 `toml:"rangeSafetyMarginLaps"`
	RangeSafetyMarginMeters float64 `toml:"rangeSafetyMarginMeters"`
}
type haptics struct {
	DynamicTransmissionFeedback  bool                                `toml:"dynamicTransmissionFeedback"`
	DynamicTransmissionCurve     int                                 `toml:"dynamicTransmissionCurve"`
	DynamicTransmissionGforceMax float64                             `toml:"dynamicTransmissionGforceMax"`
	JerkCurve                    int                                 `toml:"jerkCurve"`
	JerkMax                      int                                 `toml:"jerkMax"`
	JerkScale                    float64                             `toml:"jerkScale"`
	SnapCurve                    int                                 `toml:"snapCurve"`
	SnapMax                      int                                 `toml:"snapMax"`
	_snapScale                   float64                             `toml:"-"`
	PulseMaxAmplitude            float64                             `toml:"pulseMaxAmplitude"`
	PulseMaxFrequencyHz          float64                             `toml:"pulseMaxFrequencyHz"`
	PulseMinFrequencyHz          float64                             `toml:"pulseMinFrequencyHz"`
	_pulseWidthMax               float64                             `toml:"-"`
	_pulseWidthMin               float64                             `toml:"-"`
	EngineProfiles               map[string]appHaptics.EngineProfile `toml:"engineProfiles"`
	_engineProfile               *appHaptics.EngineProfile           `toml:"-"`
}

type hardware struct {
	Model              string `toml:"model"`
	DisplayOrientation int    `toml:"displayOrientation"`
}

type pitRadio struct {
	Enabled               bool `toml:"enabled"`
	MessageSendIntervalMs int  `toml:"messageSendIntervalMs"`
}

// Synthesizer represents an audio synthesizer used for haptic feedback.
type Synthesizer struct {
	InternalSampleRateHz      int       `toml:"internalSampleRateHz"`
	OutputSampleRateHz        int       `toml:"outputSampleRateHz"`
	OutputFile                string    `toml:"outputFile"`
	MasterGain                float64   `toml:"masterGain"`
	ChassisGain               float64   `toml:"chassisGain"`
	TransmissionGain          float64   `toml:"transmissionGain"`
	TransmissionGainMinRace   float64   `toml:"transmissionGainMinRace"`
	TransmissionGainMinStreet float64   `toml:"transmissionGainMinStreet"`
	EngineGain                float64   `toml:"engineGain"`
	GainIncrement             float64   `toml:"gainIncrement"`
	Eq                        []float64 `toml:"eq"`
}

// Telemetry represents the telemetry data source configuration.
type Telemetry struct {
	Source string `toml:"source"`
}

type tyres struct {
	MonitoringEnabled          bool    `toml:"monitoringEnabled"`
	TemperatureOptimalCelsius  float32 `toml:"temperatureOptimalCelsius"`
	TemperatureOperatingWindow float32 `toml:"temperatureOperatingWindow"`
	TemperatureMarginCelsius   float32 `toml:"temperatureMarginCelsius"`
}

type viperConfig struct {
	App         *app         `toml:"app"`
	Discord     *discord     `toml:"discord"`
	Fuel        *fuel        `toml:"fuel"`
	Hardware    *hardware    `toml:"hardware"`
	Haptics     *haptics     `toml:"haptics"`
	PitRadio    *pitRadio    `toml:"pitRadio"`
	Synthesizer *Synthesizer `toml:"synthesizer"`
	Telemetry   *Telemetry   `toml:"telemetry"`
	Tyres       *tyres       `toml:"tyres"`
}

// Config holds the application configuration and provides methods for accessing and modifying the data.
type Config struct {
	viper    *viperConfig
	i18n     *i18n.I18n
	filename string
	mu       sync.RWMutex
}

// New creates a new Config instance loading configuration from the specified filename.
func New(filename string, log zerolog.Logger) *Config {
	config := &Config{
		viper:    defaultConfig(),
		filename: filename,
	}

	viper.SetEnvPrefix("SIMTEZILO")
	viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))
	viper.AutomaticEnv()
	viper.SetConfigName(filename)
	viper.SetConfigType("toml")
	viper.AddConfigPath("/boot/firmware/simtezilo/")
	viper.AddConfigPath("/boot/simtezilo/")
	viper.AddConfigPath("/opt/simtezilo/etc/")
	viper.AddConfigPath("/opt/simtezilo/")
	viper.AddConfigPath(".")

	err := viper.ReadInConfig()
	if err != nil {
		log.Error().Err(err).Msg("read config file")
	} else {
		err = viper.Unmarshal(config.viper)
		if err != nil {
			log.Error().Err(err).Msg("unmarshal config")
		}
	}

	configSource := viper.ConfigFileUsed()
	if configSource == "" {
		configSource = "internal default"
	}

	log.Debug().Str("source", configSource).Msg("config loaded")

	config.finalise()

	return config
}

// NewFromJSON creates a new Config instance loading configuration from the provided JSON byte slice.
func NewFromJSON(json []byte, log zerolog.Logger) *Config {
	config := &Config{
		viper: defaultConfig(),
	}

	vConf := viper.New()
	vConf.SetConfigType("json")

	err := vConf.ReadConfig(bytes.NewBuffer(json))
	if err != nil {
		log.Error().Err(err).Msg("read config file")
	} else {
		err = vConf.Unmarshal(config.viper)
		if err != nil {
			log.Error().Err(err).Msg("unmarshal config")
		}
	}

	configSource := "JSON string"

	log.Debug().Str("source", configSource).Msg("config loaded")

	config.finalise()

	return config
}

// SetDefault resets the configuration to the default values.
func (c *Config) SetDefault() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper = defaultConfig()
	c.finalise()
}

// SetI18n sets the i18n instance for the Config which is required for interaction with language settings.
func (c *Config) SetI18n(i18n *i18n.I18n) {
	c.i18n = i18n
}

// GetOutputFile returns the synthesizer output file path.
func (c *Config) GetOutputFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.OutputFile
}

// ****************************************************************************
// App section methods.
// ****************************************************************************

// GetAppLanguage returns the configured application language.
// If not set, it defaults to "en".
func (c *Config) GetAppLanguage() *string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.App.Language == "" {
		c.viper.App.Language = "en"
	}

	return &c.viper.App.Language
}

// SetAppLanguage sets the application language.
func (c *Config) SetAppLanguage(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO: validate language code

	c.viper.App.Language = value
}

// GetAppAccent returns the configured accent.
// If not set, it defaults to "us".
func (c *Config) GetAppAccent() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.App.Accent == "" {
		return "us"
	}

	return c.viper.App.Accent
}

// NextLanguage cycles to the next available language.
// It returns the language code of the selected language.
func (c *Config) NextLanguage() string {
	if c.i18n == nil {
		log.Warn().Msg("i18n instance not set in config")

		return c.viper.App.Language
	}

	languageCodes := c.i18n.LanguageCodes()

	var language string

	for i, lang := range languageCodes {
		if lang == c.viper.App.Language {
			nextIndex := (i + 1) % len(languageCodes)
			language = languageCodes[nextIndex]

			break
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if language != "" {
		c.viper.App.Language = language
	} else {
		c.viper.App.Language = "en"
	}

	return c.viper.App.Language
}

// PreviousLanguage cycles to the previous available language.
// It returns the language code of the selected language.
func (c *Config) PreviousLanguage() string {
	if c.i18n == nil {
		log.Warn().Msg("i18n instance not set in config")

		return c.viper.App.Language
	}

	languageCodes := c.i18n.LanguageCodes()

	var language string

	for i, lang := range languageCodes {
		if lang == c.viper.App.Language {
			prevIndex := (i - 1 + len(languageCodes)) % len(languageCodes)
			language = languageCodes[prevIndex]

			break
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if language != "" {
		c.viper.App.Language = language
	} else {
		c.viper.App.Language = "en"
	}

	return c.viper.App.Language
}

// GetAppLogLevel returns the configured log level.
// If not set, it defaults to "info".
func (c *Config) GetAppLogLevel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.App.LogLevel == "" {
		return "info"
	}

	return c.viper.App.LogLevel
}

// SetAppLogLevel sets the application log level.
func (c *Config) SetAppLogLevel(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO: validate log level

	c.viper.App.LogLevel = value
}

// GetAppReplayMode returns true if replay mode is enabled.
func (c *Config) GetAppReplayMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.ReplayMode
}

// GetAppDataDir returns the configured application data directory.
// If not set, it defaults to "data".
func (c *Config) GetAppDataDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.App.DataDir == "" {
		return "data"
	}

	return c.viper.App.DataDir
}

// GetAppWebUIEnabled returns true if the web UI is enabled.
func (c *Config) GetAppWebUIEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.WebUIEnabled
}

// GetAppWebUIPort returns the configured web UI port.
// If not set, it defaults to 8080.
func (c *Config) GetAppWebUIPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.App.WebUIPort == 0 {
		return 8080
	}

	return c.viper.App.WebUIPort
}

// ****************************************************************************
// Discord section methods.
// ****************************************************************************

// GetDiscordEnabled returns true if Discord integration is enabled.
func (c *Config) GetDiscordEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Discord.Enabled
}

// GetDiscordToken returns the Discord API token.
func (c *Config) GetDiscordToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Discord.Token
}

// GetDiscordGuildID returns the Discord guild (server) ID.
func (c *Config) GetDiscordGuildID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Discord.GuildID
}

// GetDiscordChannelID returns the Discord text channel ID.
func (c *Config) GetDiscordChannelID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Discord.ChannelID
}

// GetDiscordVoiceChannelID returns the Discord voice channel ID.
func (c *Config) GetDiscordVoiceChannelID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Discord.VoiceChannelID
}

// ****************************************************************************
// Fuel section methods.
// ****************************************************************************

// GetFuelMonitoringEnabled returns true if fuel monitoring is enabled.
func (c *Config) GetFuelMonitoringEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Fuel.MonitoringEnabled
}

// SetFuelMonitoringEnabled sets whether fuel monitoring is enabled.
func (c *Config) SetFuelMonitoringEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fuel.MonitoringEnabled = value
}

// GetFuelPreWarnNotifyLaps returns the number of laps remaining before a fuel pre-warning is triggered.
func (c *Config) GetFuelPreWarnNotifyLaps() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Fuel.PreWarnNotifyLaps
}

// GetFuelStrategyNotifyLaps returns the number of laps remaining before a fuel strategy notification is triggered.
func (c *Config) GetFuelStrategyNotifyLaps() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Fuel.StrategyNotifyLaps
}

// GetFuelRangeSafetyMarginLaps returns the safety margin in laps to apply when calculating fuel range.
func (c *Config) GetFuelRangeSafetyMarginLaps() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Fuel.RangeSafetyMarginLaps
}

// GetFuelRangeSafetyMarginMeters returns the safety margin in meters to apply when calculating fuel range.
func (c *Config) GetFuelRangeSafetyMarginMeters() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Fuel.RangeSafetyMarginMeters
}

// ****************************************************************************
// Hardware section methods.
// ****************************************************************************

// GetHardwareModel returns the configured hardware model.
func (c *Config) GetHardwareModel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Hardware.Model
}

// GetDisplayOrientation returns the configured display orientation in degrees.
// Valid orientaitions are 0, 90, 180, and 270 degrees.
func (c *Config) GetDisplayOrientation() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Hardware.DisplayOrientation
}

// ****************************************************************************
// Haptics section methods.
// ****************************************************************************

// DynamicTransmissionFeedbackEnabled returns true if dynamic transmission feedback is enabled.
func (c *Config) DynamicTransmissionFeedbackEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.DynamicTransmissionFeedback
}

// GetJerkCurve returns the jerk curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) GetJerkCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.viper.Haptics.JerkCurve)
}

// SetJerkCurve sets the jerk curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) SetJerkCurve(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = min(value, 955)
	value = max(value, 5)

	c.viper.Haptics.JerkCurve = value
}

// IncreaseJerkCurve increases the jerk curve value in increments of 5.
func (c *Config) IncreaseJerkCurve() int {
	c.mu.Lock()

	c.viper.Haptics.JerkCurve = min(
		955,
		c.viper.Haptics.JerkCurve+5,
	)

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.JerkCurve
}

// DecreaseJerkCurve decreases the jerk curve value in increments of 5.
func (c *Config) DecreaseJerkCurve() int {
	c.mu.Lock()

	c.viper.Haptics.JerkCurve = max(
		5,
		c.viper.Haptics.JerkCurve-5,
	)

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.JerkCurve
}

// GetJerkScale returns the current jerk scale factor.
func (c *Config) GetJerkScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.JerkScale
}

// UpdateJerkScale recalculates the jerk scale factor based on the current jerk curve, scale and maximum.
func (c *Config) UpdateJerkScale() {
	exponent := c.GetJerkCurve() / 1000

	c.mu.Lock()
	defer c.mu.Unlock()

	jerkMax := 100 * float64(c.viper.Haptics.JerkMax)

	c.viper.Haptics.JerkScale = 1 / math.Pow(jerkMax, exponent)
}

// GetJerkMax returns the maximum jerk value.
// The jerk curve is applied over the range from 0 to this maximum value.
// Any jerk vakues above this value are clamped to this maximum.
func (c *Config) GetJerkMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.JerkMax
}

// SetJerkMax sets the maximum jerk value.
// The jerk curve is applied over the range from 0 to this maximum value.
// Any jerk vakues above this value are clamped to this maximum.
func (c *Config) SetJerkMax(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = min(value, 200)
	value = max(value, 1)

	c.viper.Haptics.JerkMax = value
}

// IncreaseJerkMax increases the maximum jerk value in increments of 1.
func (c *Config) IncreaseJerkMax() int {
	c.mu.Lock()

	c.viper.Haptics.JerkMax = min(
		100,
		c.viper.Haptics.JerkMax+1,
	)

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.JerkMax
}

// DecreaseJerkMax decreases the maximum jerk value in increments of 1.
func (c *Config) DecreaseJerkMax() int {
	c.mu.Lock()

	c.viper.Haptics.JerkMax = max(
		1,
		c.viper.Haptics.JerkMax-1,
	)

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.JerkMax
}

// GetSnapCurve returns the snap curve value.
func (c *Config) GetSnapCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.viper.Haptics.SnapCurve)
}

// SetSnapCurve sets the snap curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) SetSnapCurve(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = min(value, 955)
	value = max(value, 5)

	c.viper.Haptics.SnapCurve = value
}

// IncreaseSnapCurve increases the snap curve value in increments of 5.
func (c *Config) IncreaseSnapCurve() int {
	c.mu.Lock()

	c.viper.Haptics.SnapCurve = min(
		955,
		c.viper.Haptics.SnapCurve+5,
	)

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.SnapCurve
}

// DecreaseSnapCurve decreases the snap curve value in increments of 5.
func (c *Config) DecreaseSnapCurve() int {
	c.mu.Lock()

	if c.viper.Haptics.SnapCurve >= 10 {
		c.viper.Haptics.SnapCurve -= 5
	} else {
		c.viper.Haptics.SnapCurve = 5
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.SnapCurve
}

// GetSnapScale returns the current snap scale factor.
func (c *Config) GetSnapScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics._snapScale
}

// UpdateSnapScale recalculates the snap scale factor based on the current snap curve, scale and maximum.
func (c *Config) UpdateSnapScale() {
	exponent := c.GetSnapCurve() / 1000

	c.mu.Lock()
	defer c.mu.Unlock()

	snapMax := 1000 * float64(c.viper.Haptics.SnapMax)

	c.viper.Haptics._snapScale = 1 / math.Pow(snapMax, exponent)
}

// GetSnapMax returns the maximum snap value.
func (c *Config) GetSnapMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.SnapMax
}

// SetSnapMax sets the maximum snap value.
// The snap curve is applied over the range from 0 to this maximum value.
// Any snap values above this value are clamped to this maximum.
// Allowed range is 1 to 200.
func (c *Config) SetSnapMax(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = min(value, 200)
	value = max(value, 1)

	c.viper.Haptics.SnapMax = value
}

// IncreaseSnapMax increases the maximum snap value in increments of 1.
func (c *Config) IncreaseSnapMax() int {
	c.mu.Lock()

	c.viper.Haptics.SnapMax = min(
		100,
		c.viper.Haptics.SnapMax+1,
	)

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.SnapMax
}

// DecreaseSnapMax decreases the maximum snap value in increments of 1.
func (c *Config) DecreaseSnapMax() int {
	c.mu.Lock()

	c.viper.Haptics.SnapMax = max(
		1,
		c.viper.Haptics.SnapMax-1,
	)

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.SnapMax
}

// GetTransmissionCurve returns the transmission curve value.
// This curve is applied to the dynamic transmission feedback bsaed on the longitudinal vehicle g-force.
func (c *Config) GetTransmissionCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.viper.Haptics.DynamicTransmissionCurve)
}

// SetTransmissionCurve sets the transmission curve value.
// This curve is applied to the dynamic transmission feedback bsaed on the longitudinal vehicle g-force.
func (c *Config) SetTransmissionCurve(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = min(value, 955)
	value = max(value, 5)

	c.viper.Haptics.DynamicTransmissionCurve = value
}

// IncreaseTransmissionCurve increases the transmission curve value in increments of 5.
func (c *Config) IncreaseTransmissionCurve() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.DynamicTransmissionCurve = min(
		955,
		c.viper.Haptics.DynamicTransmissionCurve+5,
	)

	return c.viper.Haptics.DynamicTransmissionCurve
}

// DecreaseTransmissionCurve decreases the transmission curve value in increments of 5.
func (c *Config) DecreaseTransmissionCurve() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.DynamicTransmissionCurve = max(
		5,
		c.viper.Haptics.DynamicTransmissionCurve-5,
	)

	return c.viper.Haptics.DynamicTransmissionCurve
}

// GetTransmissionGforceMax returns the maximum g-force for dynamic transmission feedback.
// Any longitudinal g-force values above this are clamped to this maximum.
func (c *Config) GetTransmissionGforceMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.DynamicTransmissionGforceMax
}

// SetTransmissionGforceMax sets the maximum transmission G-force value.
// Any longitudinal g-force values above this are clamped to this maximum.
func (c *Config) SetTransmissionGforceMax(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = math.Min(10, value)
	value = math.Max(0, value)

	c.viper.Haptics.DynamicTransmissionGforceMax = value
}

// IncreaseTransmissionGforceMax increases the maximum g-force for dynamic transmission feedback in increments of 0.1g.
func (c *Config) IncreaseTransmissionGforceMax() float64 {
	c.mu.Lock()

	c.viper.Haptics.DynamicTransmissionGforceMax = min(
		5.0,
		c.viper.Haptics.DynamicTransmissionGforceMax+0.1,
	)

	c.mu.Unlock()

	return c.viper.Haptics.DynamicTransmissionGforceMax
}

// DecreaseTransmissionGforceMax decreases the maximum g-force for dynamic transmission feedback in increments of 0.1g.
func (c *Config) DecreaseTransmissionGforceMax() float64 {
	c.mu.Lock()

	c.viper.Haptics.DynamicTransmissionGforceMax = max(
		0.1,
		c.viper.Haptics.DynamicTransmissionGforceMax-0.1,
	)

	c.mu.Unlock()

	return c.viper.Haptics.DynamicTransmissionGforceMax
}

// GetMinHz returns the configured minimum pulse frequency in Hz.
// This is the minimum frequency output for chassis bump haptics.
func (c *Config) GetMinHz() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.PulseMinFrequencyHz
}

// GetEngineProfile returns the currently selected engine profile.
// If no profile is selected, it returns nil.
func (c *Config) GetEngineProfile(name string) *appHaptics.EngineProfile {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = strings.ToLower(name)
	if profile, ok := c.viper.Haptics.EngineProfiles[name]; ok {
		c.viper.Haptics._engineProfile = &profile
	} else {
		c.viper.Haptics._engineProfile = nil
	}

	return c.viper.Haptics._engineProfile
}

// GetEnginePrimaryBalance returns the current engine primary balance.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) GetEnginePrimaryBalance() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	return c.viper.Haptics._engineProfile.PrimaryBalance
}

// IncreaseEnginePrimaryBalance increases the current engoine primary balancee in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) IncreaseEnginePrimaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	c.viper.Haptics._engineProfile.PrimaryBalance = min(
		1.0,
		c.viper.Haptics._engineProfile.PrimaryBalance+0.01,
	)

	return c.viper.Haptics._engineProfile.PrimaryBalance
}

// DecreaseEnginePrimaryBalance decreases the current engine primary balance in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) DecreaseEnginePrimaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	c.viper.Haptics._engineProfile.PrimaryBalance = max(
		0.0,
		c.viper.Haptics._engineProfile.PrimaryBalance-0.01,
	)

	return c.viper.Haptics._engineProfile.PrimaryBalance
}

// GetEngineSecondaryBalance returns the current engine secondary balance.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) GetEngineSecondaryBalance() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	return c.viper.Haptics._engineProfile.SecondaryBalance
}

// IncreaseEngineSecondaryBalance increases the current engine secondary balance in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) IncreaseEngineSecondaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	c.viper.Haptics._engineProfile.SecondaryBalance = min(
		1.0,
		c.viper.Haptics._engineProfile.SecondaryBalance+0.01,
	)

	return c.viper.Haptics._engineProfile.SecondaryBalance
}

// DecreaseEngineSecondaryBalance decreases the current engine secondary balance in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) DecreaseEngineSecondaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	c.viper.Haptics._engineProfile.SecondaryBalance = max(
		0.0,
		c.viper.Haptics._engineProfile.SecondaryBalance-0.01,
	)

	return c.viper.Haptics._engineProfile.SecondaryBalance
}

// GetEnginePulseGain returns the current engine pulse gain (i.e. engine haptic volume).
// If no profile is selected, it returns a gain level that silences engine haptics.
func (c *Config) GetEnginePulseGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return MinimumGain
	}

	return c.viper.Haptics._engineProfile.Gain
}

// IncreaseEnginePulseGain increases the current engine pulse gain by the configured increment.
// If no profile is selected, it returns a gain level that silences engine haptics.
func (c *Config) IncreaseEnginePulseGain() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return MinimumGain
	}

	c.viper.Haptics._engineProfile.Gain = min(
		MaximumGain,
		c.viper.Haptics._engineProfile.Gain+c.viper.Synthesizer.GainIncrement,
	)

	return c.viper.Haptics._engineProfile.Gain
}

// DecreaseEnginePulseGain decreases the current engine pulse gain by the configured increment.
// If no profile is selected, it returns a gain level that silences engine haptics.
func (c *Config) DecreaseEnginePulseGain() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return MinimumGain
	}

	c.viper.Haptics._engineProfile.Gain = max(
		MinimumGain,
		c.viper.Haptics._engineProfile.Gain-c.viper.Synthesizer.GainIncrement,
	)

	return c.viper.Haptics._engineProfile.Gain
}

// GetEnginePulseScale returns the current engine pulse scale factor.
// If no profile is selected, it returns a scale factor of 1.0 (no scaling).
func (c *Config) GetEnginePulseScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	return c.viper.Haptics._engineProfile.PulseScale
}

// IncreaseEnginePulseScale increases the current engine pulse scale factor in increments of 0.01.
// If no profile is selected, it returns a scale factor of 1.0 (no scaling).
func (c *Config) IncreaseEnginePulseScale() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	c.viper.Haptics._engineProfile.PulseScale = min(
		1.0,
		c.viper.Haptics._engineProfile.PulseScale+0.01,
	)

	return c.viper.Haptics._engineProfile.PulseScale
}

// DecreaseEnginePulseScale decreases the current engine pulse scale factor in increments of 0.01.
// If no profile is selected, it returns a scale factor of 1.0 (no scaling).
func (c *Config) DecreaseEnginePulseScale() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	c.viper.Haptics._engineProfile.PulseScale = max(
		0.0,
		c.viper.Haptics._engineProfile.PulseScale-0.01,
	)

	return c.viper.Haptics._engineProfile.PulseScale
}

// IncreaseMinHz increases the minimum pulse frequency in 1 Hz increments.
// This is the minimum frequency output for chassis bump haptics and is clamped to a maximum of 25Hz.
func (c *Config) IncreaseMinHz() int {
	c.mu.Lock()

	c.viper.Haptics.PulseMinFrequencyHz = min(25, c.viper.Haptics.PulseMinFrequencyHz+1)

	c.updatePulseWidthExtents()

	c.mu.Unlock()

	return int(c.viper.Haptics.PulseMinFrequencyHz)
}

// DecreaseMinHz decreases the minimum pulse frequency in 1 Hz increments.
// This is the minimum frequency output for chassis bump haptics and is clamped to a minimum of 5Hz.
func (c *Config) DecreaseMinHz() int {
	c.mu.Lock()

	c.viper.Haptics.PulseMinFrequencyHz = max(5, c.viper.Haptics.PulseMinFrequencyHz-1)

	c.updatePulseWidthExtents()

	c.mu.Unlock()

	return int(c.viper.Haptics.PulseMinFrequencyHz)
}

// GetMaxHz returns the configured maximum pulse frequency in Hz.
// This is the maximum frequency output for chassis bump haptics.
func (c *Config) GetMaxHz() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.PulseMaxFrequencyHz
}

// IncreaseMaxHz increases the maximum pulse frequency in 1 Hz increments.
// This is the maximum frequency output for chassis bump haptics and is clamped to a maximum of 100Hz.
func (c *Config) IncreaseMaxHz() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.PulseMaxFrequencyHz = min(100, c.viper.Haptics.PulseMaxFrequencyHz+1)

	c.updatePulseWidthExtents()

	return int(c.viper.Haptics.PulseMaxFrequencyHz)
}

// DecreaseMaxHz decreases the maximum pulse frequency in 1 Hz increments.
// This is the maximum frequency output for chassis bump haptics and is clamped to a minimum of 26Hz.
func (c *Config) DecreaseMaxHz() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.PulseMaxFrequencyHz = max(26, c.viper.Haptics.PulseMaxFrequencyHz-1)

	c.updatePulseWidthExtents()

	return int(c.viper.Haptics.PulseMaxFrequencyHz)
}

// GetFrequencyHzRange returns the range between the configured minimum and maximum pulse frequencies in Hz.
// This is the frequency range output for chassis bump haptics.
func (c *Config) GetFrequencyHzRange() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.PulseMaxFrequencyHz - c.viper.Haptics.PulseMinFrequencyHz
}

// GetPulseWidthMin returns the minimum pulse width in samples based on the current max frequency.
func (c *Config) GetPulseWidthMin() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics._pulseWidthMin
}

// GetPulseWidthMax returns the maximum pulse width in samples based on the current min and max frequencies.
func (c *Config) GetPulseWidthMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics._pulseWidthMax
}

// GetPulseMaxAmplitude returns the maximum pulse amplitude for chassis bump haptics.
func (c *Config) GetPulseMaxAmplitude() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.PulseMaxAmplitude
}

// ****************************************************************************
// Pit Radio section methods.
// ****************************************************************************

// PitRadioEnabled returns true if pit radio integration is enabled.
func (c *Config) PitRadioEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Enabled
}

// GetMessageSendIntervalMs returns the interval in milliseconds between sending of pit radio messages.
func (c *Config) GetMessageSendIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.MessageSendIntervalMs
}

// ****************************************************************************
// Synthesizer methods.
// ****************************************************************************

// GetSynthesizer returns the synthesizer configuration.
func (c *Config) GetSynthesizer() *Synthesizer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer
}

// GetInternalSampleRateHz returns the internal sample rate of the synthesizer in Hz.
// This is the sample rate at which the synthesizer processes audio.
// Lower values reduce CPU load and 8000 Hz should be more than sufficient for the haptic frequency range.
func (c *Config) GetInternalSampleRateHz() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.InternalSampleRateHz
}

// GetOutputSampleRateHz returns the output sample rate of the synthesizer in Hz.
// This is the sample rate at which audio is output to the audio device or file.
// 32000 Hz is suitable for most common hardware but some may work at lower rates.
func (c *Config) GetOutputSampleRateHz() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.OutputSampleRateHz
}

// GetGainIncrement returns the gain increment value.
func (c *Config) GetGainIncrement() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.GainIncrement
}

// SetGainIncrement sets the gain increment value.
func (c *Config) SetGainIncrement(value float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	value = max(0.01, value)
	value = min(10, value)

	c.viper.Synthesizer.GainIncrement = value
}

// GetMasterGain returns the master gain of the synthesizer (i.e. the overall volume level).
// This is a global gain applied to all haptic feedback.
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) GetMasterGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.MasterGain
}

// SetMasterGain sets the master gain of the synthesizer.
// This is a global gain applied to all haptic feedback.
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) SetMasterGain(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Synthesizer.MasterGain = max(MinimumGain, min(MaximumGain, value))
}

// IncreaseMasterGain increases the master gain by the configured gain increment.
func (c *Config) IncreaseMasterGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.MasterGain = min(
		MaximumGain,
		c.viper.Synthesizer.MasterGain+c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.MasterGain
}

// DecreaseMasterGain decreases the master gain by the configured gain increment.
func (c *Config) DecreaseMasterGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.MasterGain = max(
		MinimumGain,
		c.viper.Synthesizer.MasterGain-c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.MasterGain
}

// GetChassisGain returns the chassis gain of the synthesizer (i.e. the volume level for chassis bump haptics).
// 0.0 is maximum gain and -60.0 will mute chassis bump haptic output.
func (c *Config) GetChassisGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.ChassisGain
}

// SetChassisGain sets the chassis gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute chassis bump haptic output.
func (c *Config) SetChassisGain(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Synthesizer.ChassisGain = max(MinimumGain, min(MaximumGain, value))
}

// IncreaseChassisGain increases the chassis gain by the configured gain increment.
func (c *Config) IncreaseChassisGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.ChassisGain = min(
		MaximumGain,
		c.viper.Synthesizer.ChassisGain+c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.ChassisGain
}

// DecreaseChassisGain decreases the chassis gain by the configured gain increment.
func (c *Config) DecreaseChassisGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.ChassisGain = max(
		MinimumGain,
		c.viper.Synthesizer.ChassisGain-c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.ChassisGain
}

// GetTransmissionGain returns the transmission gain of the synthesizer (i.e. the volume level for transmission
// haptics).
// 0.0 is maximum gain and -60.0 will mute transmission haptic output.
func (c *Config) GetTransmissionGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.TransmissionGain
}

// SetTransmissionGain sets the transmission gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute transmission haptic output.
func (c *Config) SetTransmissionGain(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Synthesizer.TransmissionGain = max(MinimumGain, min(MaximumGain, value))
}

// IncreaseTransmissionGain increases the transmission gain by the configured gain increment.
func (c *Config) IncreaseTransmissionGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.TransmissionGain = min(
		MaximumGain,
		c.viper.Synthesizer.TransmissionGain+c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.TransmissionGain
}

// DecreaseTransmissionGain decreases the transmission gain by the configured gain increment.
func (c *Config) DecreaseTransmissionGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.TransmissionGain = max(
		MinimumGain,
		c.viper.Synthesizer.TransmissionGain-c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.TransmissionGain
}

// GetTransmissionGainMinRace returns the minimum transmission gain for race vehicle types.
// This is the transnmission haptic feebdack volume applied when the vehicle is stationary.
func (c *Config) GetTransmissionGainMinRace() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.TransmissionGainMinRace
}

// GetTransmissionGainMinStreet returns the minimum transmission gain for street vehicle types.
// This is the transmission haptic feedback volume applied when the vehicle is stationary.
func (c *Config) GetTransmissionGainMinStreet() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.TransmissionGainMinStreet
}

// GetEngineGain returns the gain fir the currently selected engine (i.e. the volume level for engine haptics).
// 0.0 is maximum gain and -60.0 will mute engine haptic output.
func (c *Config) GetEngineGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.EngineGain
}

// SetEngineGain sets the engine gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute engine haptic output.
func (c *Config) SetEngineGain(gain float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Synthesizer.EngineGain = max(MinimumGain, min(MaximumGain, gain))
}

// IncreaseEngineGain increases the gain of the currently selected engine by the configured gain increment.
func (c *Config) IncreaseEngineGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.EngineGain = min(
		MaximumGain,
		c.viper.Synthesizer.EngineGain+c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.EngineGain
}

// DecreaseEngineGain decreases the gain of the currently selected engine by the configured gain increment.
func (c *Config) DecreaseEngineGain() float64 {
	c.mu.Lock()

	c.viper.Synthesizer.EngineGain = max(
		MinimumGain,
		c.viper.Synthesizer.EngineGain-c.viper.Synthesizer.GainIncrement,
	)

	c.mu.Unlock()

	return c.viper.Synthesizer.EngineGain
}

// ****************************************************************************
// Telemetry methods.
// ****************************************************************************

// GetTelemetrySource returns the configured telemetry source.
func (c *Config) GetTelemetrySource() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Telemetry.Source
}

// SetTelemetrySource sets the telemetry source.
func (c *Config) SetTelemetrySource(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO: validate value

	c.viper.Telemetry.Source = value
}

// ****************************************************************************
// Tyres section methods.
// ****************************************************************************

// GetTyreMonitoringEnabled returns whether tyre monitoring is enabled.
func (c *Config) GetTyreMonitoringEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Tyres.MonitoringEnabled
}

// GetTyreTemperatureOptimalCelsius returns the optimal (center) tyre temperature in Celsius.
func (c *Config) GetTyreTemperatureOptimalCelsius() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Tyres.TemperatureOptimalCelsius
}

// GetTyreTemperatureOperatingWindow returns the total operating window width around optimal temperature in Celsius.
// The ideal temperature range is calculated as optimal ± (window/2).
func (c *Config) GetTyreTemperatureOperatingWindow() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Tyres.TemperatureOperatingWindow
}

// GetTyreTemperatureMarginCelsius returns the margin beyond operating window for hot/cold thresholds in Celsius.
func (c *Config) GetTyreTemperatureMarginCelsius() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Tyres.TemperatureMarginCelsius
}

// ****************************************************************************
// Configuration file management methods.
// ****************************************************************************

// BackupConfigFile creates a backup of the current configuration file.
// Returns the backup filename and any error encountered.
func (c *Config) BackupConfigFile() (string, error) {
	configPath := c.resolveConfigFilePath()

	// If the file doesn't exist, nothing to back up
	_, err := os.Stat(configPath)
	if err != nil {
		return "", fmt.Errorf("configuration file %s not found", configPath)
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup.%s", configPath, timestamp)

	source, err := os.Open(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	if err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	return backupPath, nil
}

// SaveConfigToFile saves the current configuration to the configuration file.
// It creates a backup before saving and returns both the backup filename and any error.
func (c *Config) SaveConfigToFile() (string, error) {
	c.mu.RLock()
	config := c.viper
	c.mu.RUnlock()

	configPath := c.resolveConfigFilePath()

	// Create backup first
	backupPath, err := c.BackupConfigFile()
	if err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	// Marshal the configuration to TOML
	tomlData, err := toml.Marshal(config)
	if err != nil {
		return backupPath, fmt.Errorf("failed to marshal configuration to TOML: %w", err)
	}

	// Write to temporary file first
	tempPath := configPath + ".tmp"

	err = os.WriteFile(tempPath, tomlData, 0o600)
	if err != nil {
		return backupPath, fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Atomic rename to replace the original file
	err = os.Rename(tempPath, configPath)
	if err != nil {
		os.Remove(tempPath)

		return backupPath, fmt.Errorf("failed to replace configuration file: %w", err)
	}

	return backupPath, nil
}

// ****************************************************************************
// Helper methods.
// ****************************************************************************

// finalise performs validation of the config and updates any derived configuration values.
func (c *Config) finalise() {
	if len(c.viper.Synthesizer.Eq) != 40 {
		log.Warn().Int("length", len(c.viper.Synthesizer.Eq)).Msg("invalid synthesizer EQ length")

		c.viper.Synthesizer.Eq = make([]float64, 40)
		for i := range 40 {
			c.viper.Synthesizer.Eq[i] = 1
		}
	}

	c.updatePulseWidthExtents()

	c.UpdateJerkScale()
	c.UpdateSnapScale()
}

// updatePulseWidthExtents recalculates the minimum and maximum pulse widths in samples.
func (c *Config) updatePulseWidthExtents() {
	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMaxFrequencyHz)

	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMinFrequencyHz)
}

// resolveConfigFilePath finds the config file in standard locations or returns a default path for writing.
func (c *Config) resolveConfigFilePath() string {
	c.mu.RLock()
	filename := c.filename
	c.mu.RUnlock()

	searchPaths := []string{
		filepath.Join("/boot/simtezilo/", filename),
		filepath.Join("/opt/simtezilo/", filename),
		filepath.Join(".", filename),
	}

	for _, path := range searchPaths {
		_, err := os.Stat(path)
		if err != nil {
			return path
		}
	}

	// If not found, return default path in current directory (for writing new file)
	return filepath.Join(".", filename)
}
