// Package config provides configuration management for the application.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	appHaptics "github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
)

type app struct {
	Language      string `toml:"language"`
	Accent        string `toml:"accent"`
	LogLevel      string `toml:"logLevel"`
	BaseDir       string `toml:"baseDir"`
	VehicleDBFile string `toml:"vehicleDBFile"`
	ReplayMode    bool   `toml:"replayMode"`
	WebUIEnabled  bool   `toml:"webUIEnabled"`
	WebUIPort     int    `toml:"webUIPort"`
}

type discord struct {
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
	_jerkScale                   float64                             `toml:"-"`
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
	Enabled               bool     `toml:"enabled"`
	MessageSendIntervalMs int      `toml:"messageSendIntervalMs"`
	Discord               *discord `toml:"discord"`
}

// EQBand represents a parametric equalizer band with center frequency, gain, and Q factor.
type EQBand struct {
	Frequency float64 `toml:"frequency"` // Center frequency in Hz
	Gain      float64 `toml:"gain"`      // Gain in dB (-12 to +6)
	Q         float64 `toml:"q"`         // Q factor (0.1 to 20, higher = narrower)
}

// Synthesizer represents an audio synthesizer used for haptic feedback.
type Synthesizer struct {
	InternalSampleRateHz      int       `toml:"internalSampleRateHz"`
	OutputSampleRateHz        int       `toml:"outputSampleRateHz"`
	OutputFile                string    `toml:"outputFile"`
	MasterGain                float64   `toml:"masterGain"`
	MasterGainMute            bool      `toml:"masterGainMute"`
	ChassisGain               float64   `toml:"chassisGain"`
	ChassisGainMute           bool      `toml:"chassisGainMute"`
	TransmissionGain          float64   `toml:"transmissionGain"`
	TransmissionGainMute      bool      `toml:"transmissionGainMute"`
	TransmissionGainMinRace   float64   `toml:"transmissionGainMinRace"`
	TransmissionGainMinStreet float64   `toml:"transmissionGainMinStreet"`
	EngineGain                float64   `toml:"engineGain"`
	EngineGainMute            bool      `toml:"engineGainMute"`
	GainIncrement             float64   `toml:"gainIncrement"`
	EqEnabled                 bool      `toml:"eqEnabled"`
	EqBands                   []EQBand  `toml:"eqBands"`
	_eqCurve                  []float64 `toml:"-"` // Computed curve for fast lookup
	_eqMinFreq                float64   `toml:"-"` // Minimum frequency for curve
	_eqMaxFreq                float64   `toml:"-"` // Maximum frequency for curve
	_eqResolution             float64   `toml:"-"` // Frequency resolution (Hz per bucket)
}

// Telemetry represents the telemetry data source configuration.
type Telemetry struct {
	Source string `toml:"source"`
}

// Status represents the status of the configuration.
type Status struct {
	LastUpdate      int64
	RestartRequired bool
}

type tyres struct {
	MonitoringEnabled          bool    `toml:"monitoringEnabled"`
	TemperatureOptimalCelsius  float32 `toml:"temperatureOptimalCelsius"`
	TemperatureOperatingWindow float32 `toml:"temperatureOperatingWindow"`
	TemperatureMarginCelsius   float32 `toml:"temperatureMarginCelsius"`
}

type viperConfig struct {
	App         *app         `toml:"app"`
	Fuel        *fuel        `toml:"fuel"`
	Hardware    *hardware    `toml:"hardware"`
	Haptics     *haptics     `toml:"haptics"`
	PitRadio    *pitRadio    `toml:"pitRadio"`
	Synthesizer *Synthesizer `toml:"synthesizer"`
	Telemetry   *Telemetry   `toml:"telemetry"`
	Tyres       *tyres       `toml:"tyres"`
}

// ConfigSnapshot holds frequently-accessed configuration values for lock-free reads.
type ConfigSnapshot struct {
	// Synthesizer gain settings
	MasterGain                float64
	MasterGainMute            bool
	ChassisGain               float64
	ChassisGainMute           bool
	TransmissionGain          float64
	TransmissionGainMute      bool
	TransmissionGainMinRace   float64
	TransmissionGainMinStreet float64
	EngineGain                float64
	EngineGainMute            bool
	GainIncrement             float64
	InternalSampleRateHz      int
	OutputSampleRateHz        int

	// Haptics jerk settings (chassis amplitude)
	JerkCurve float64
	JerkMax   int
	JerkScale float64

	// Haptics snap settings (chassis frequency)
	SnapCurve float64
	SnapMax   int
	SnapScale float64

	// Haptics pulse settings
	PulseMaxAmplitude   float64
	PulseMaxFrequencyHz float64
	PulseMinFrequencyHz float64
	PulseWidthMin       float64
	PulseWidthMax       float64

	// Dynamic transmission settings
	DynamicTransmissionFeedback  bool
	DynamicTransmissionCurve     int
	DynamicTransmissionGforceMax float64

	// EQ settings
	EqEnabled bool

	// Monitoring flags
	FuelMonitoringEnabled bool
	TyreMonitoringEnabled bool

	// Hardware settings
	DisplayOrientation int
}

// Config holds the application configuration and provides methods for accessing and modifying the data.
type Config struct {
	viper           *viperConfig
	snapshot        atomic.Pointer[ConfigSnapshot]
	i18n            *i18n.I18n
	configFile      string
	lastSavedConfig []byte // Last config written to disk (to avoid unnecessary writes)
	status          Status
	mu              sync.RWMutex
}

type Options struct {
	ConfigFile string
	Logger     zerolog.Logger
}

// New creates a new Config instance loading configuration from the specified filename.
func New(opts Options) *Config {
	config := &Config{
		viper:      defaultConfig(),
		configFile: opts.ConfigFile,
		status: Status{
			RestartRequired: false,
			LastUpdate:      0,
		},
	}

	viper.SetEnvPrefix("SIMTEZILO")
	viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))
	viper.AutomaticEnv()
	viper.SetConfigType("toml")

	if opts.ConfigFile != "" {
		opts.Logger.Debug().Str("filename", opts.ConfigFile).Msg("Loading config file")

		viper.SetConfigFile(opts.ConfigFile)
	} else {
		opts.Logger.Debug().Msg("No config file specified, searching default locations")

		viper.SetConfigName("simtezilo.conf")
		viper.AddConfigPath("/boot/firmware/simtezilo/")
		viper.AddConfigPath("/boot/simtezilo/")
		viper.AddConfigPath("/opt/simtezilo/etc/")
		viper.AddConfigPath("/opt/simtezilo/")
		viper.AddConfigPath(".")
	}

	err := viper.ReadInConfig()
	if err != nil {
		log.Error().
			Str("filename", viper.ConfigFileUsed()).
			Err(err).
			Msg("read config file")
	} else {
		err = viper.Unmarshal(config.viper)
		if err != nil {
			log.Error().Err(err).Msg("unmarshal config")
		}

		config.configFile = viper.ConfigFileUsed()
		log.Debug().Str("source", config.configFile).Msg("config loaded")

		// Initialize lastSavedConfig with current state to prevent false restart indicators
		tomlData, err := toml.Marshal(config.viper)
		if err == nil {
			config.lastSavedConfig = tomlData
		}
	}

	// When config is loaded from defaults set a default config file for file save operations
	if config.configFile == "" {
		config.configFile = filepath.Join(".", "simtezilo.conf")
	}

	config.finalise()
	config.rebuildSnapshot()

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
	config.rebuildSnapshot()

	return config
}

// SetDefault resets the configuration to the default values.
func (c *Config) SetDefault() {
	c.mu.Lock()

	// Try to load default config from <baseDir>/etc/default.conf
	baseDir := c.viper.App.BaseDir
	if baseDir == "" {
		baseDir = "."
	}

	defaultConfigPath := filepath.Join(baseDir, "etc", "default.conf")

	// Check if the file exists
	if _, err := os.Stat(defaultConfigPath); err == nil {
		// File exists, try to load it
		data, err := os.ReadFile(defaultConfigPath)
		if err == nil {
			// Create a new config structure
			newConfig := defaultConfig()

			err = toml.Unmarshal(data, newConfig)
			if err == nil {
				c.viper = newConfig
				c.mu.Unlock()
				c.finalise()
				c.rebuildSnapshot()

				return
			}
			// If unmarshal failed, fall through to default
			log.Warn().Err(err).Str("file", defaultConfigPath).Msg("failed to unmarshal default config, using built-in defaults")
		} else {
			log.Warn().Err(err).Str("file", defaultConfigPath).Msg("failed to read default config, using built-in defaults")
		}
	}

	// Fall back to built-in defaults
	c.viper = defaultConfig()

	c.mu.Unlock()

	c.finalise()
	c.rebuildSnapshot()
}

// SetI18n sets the i18n instance for the Config which is required for interaction with language settings.
func (c *Config) SetI18n(i18n *i18n.I18n) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.i18n = i18n
}

// GetI18n returns the i18n instance.
func (c *Config) GetI18n() *i18n.I18n {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.i18n
}

// Status returns the current configuration status.
// The status includes Unix timestamp of the last change and whether a restart is required to apply the config.
func (c *Config) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.status
}

// IsUpToDate returns true if the configuration has changed since the given Unix timestamp.
func (c *Config) IsUpToDate(timestamp int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return timestamp < c.status.LastUpdate
}

// RestartRequired returns true if a restart is required for configuration changes to take effect.
func (c *Config) RestartRequired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.status.RestartRequired
}

// MarkRestartRequired marks that a restart is required for configuration changes to take effect.
func (c *Config) MarkRestartRequired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.status.RestartRequired = true
	c.status.LastUpdate = time.Now().Unix()
}

// ****************************************************************************
// App section methods.
// ****************************************************************************

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

// SetAppAccent sets the application accent.
func (c *Config) SetAppAccent(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.Accent = value

	c.registerUpdate(false)
}

// GetAppBaseDir returns the configured base directory.
// If not set, it defaults to the current directory (".").
func (c *Config) GetAppBaseDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.App.BaseDir == "" {
		return "."
	}

	return c.viper.App.BaseDir
}

// SetAppBaseDir sets the application base directory.
func (c *Config) SetAppBaseDir(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.BaseDir = value

	c.registerUpdate(true)
}

// GetAppVehicleDBFile returns the configured vehicle database file path.
func (c *Config) GetAppVehicleDBFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.VehicleDBFile
}

// SetAppVehicleDBFile sets the vehicle database file path.
func (c *Config) SetAppVehicleDBFile(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.VehicleDBFile = value

	c.registerUpdate(true)
}

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

	c.registerUpdate(false)
}

// NextAppLanguage cycles to the next available language.
// It returns the language code of the selected language.
func (c *Config) NextAppLanguage() string {
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

	c.registerUpdate(false)

	return c.viper.App.Language
}

// PreviousAppLanguage cycles to the previous available language.
// It returns the language code of the selected language.
func (c *Config) PreviousAppLanguage() string {
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

	c.registerUpdate(false)

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

	c.registerUpdate(true)
}

// GetAppReplayMode returns true if replay mode is enabled.
func (c *Config) GetAppReplayMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.ReplayMode
}

// SetAppReplayMode sets whether replay mode is enabled.
func (c *Config) SetAppReplayMode(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.ReplayMode = value

	c.registerUpdate(true)
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

// GetDiscordToken returns the Discord API token.
func (c *Config) GetDiscordToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Discord.Token
}

// SetDiscordToken sets the Discord API token.
func (c *Config) SetDiscordToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Discord.Token = token

	c.registerUpdate(true)
}

// GetDiscordGuildID returns the Discord guild (server) ID.
func (c *Config) GetDiscordGuildID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Discord.GuildID
}

// SetDiscordGuildID sets the Discord guild (server) ID.
func (c *Config) SetDiscordGuildID(guildID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Discord.GuildID = guildID

	c.registerUpdate(true)
}

// GetDiscordChannelID returns the Discord text channel ID.
func (c *Config) GetDiscordChannelID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Discord.ChannelID
}

// SetDiscordChannelID sets the Discord text channel ID.
func (c *Config) SetDiscordChannelID(channelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Discord.ChannelID = channelID

	c.registerUpdate(true)
}

// GetDiscordVoiceChannelID returns the Discord voice channel ID.
func (c *Config) GetDiscordVoiceChannelID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Discord.VoiceChannelID
}

// SetDiscordVoiceChannelID sets the Discord voice channel ID.
func (c *Config) SetDiscordVoiceChannelID(voiceChannelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Discord.VoiceChannelID = voiceChannelID

	c.registerUpdate(true)
}

// ****************************************************************************
// Fuel section methods.
// ****************************************************************************

// GetFuelMonitoringEnabled returns true if fuel monitoring is enabled.
func (c *Config) GetFuelMonitoringEnabled() bool {
	return c.snapshot.Load().FuelMonitoringEnabled
}

// SetFuelMonitoringEnabled sets whether fuel monitoring is enabled.
func (c *Config) SetFuelMonitoringEnabled(value bool) {
	c.mu.Lock()
	c.viper.Fuel.MonitoringEnabled = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
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

// SetFuelPreWarnNotifyLaps sets the number of laps remaining before a fuel pre-warning is triggered.
func (c *Config) SetFuelPreWarnNotifyLaps(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fuel.PreWarnNotifyLaps = value

	c.registerUpdate(false)
}

// SetFuelStrategyNotifyLaps sets the number of laps remaining before a fuel strategy notification is triggered.
func (c *Config) SetFuelStrategyNotifyLaps(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fuel.StrategyNotifyLaps = value

	c.registerUpdate(false)
}

// SetFuelRangeSafetyMarginLaps sets the safety margin in laps to apply when calculating fuel range.
func (c *Config) SetFuelRangeSafetyMarginLaps(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fuel.RangeSafetyMarginLaps = value

	c.registerUpdate(false)
}

// SetFuelRangeSafetyMarginMeters sets the safety margin in meters to apply when calculating fuel range.
func (c *Config) SetFuelRangeSafetyMarginMeters(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fuel.RangeSafetyMarginMeters = value

	c.registerUpdate(false)
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

// SetHardwareModel sets the hardware model.
func (c *Config) SetHardwareModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Hardware.Model = model

	c.registerUpdate(true)
}

// GetDisplayOrientation returns the configured display orientation in degrees.
// Valid orientaitions are 0, 90, 180, and 270 degrees.
// Uses lock-free atomic read from snapshot.
func (c *Config) GetDisplayOrientation() int {
	return c.snapshot.Load().DisplayOrientation
}

// getDisplayOrientationLocked returns the display orientation while holding the lock.
// Used internally during initialization before snapshot is built.
func (c *Config) getDisplayOrientationLocked() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Hardware.DisplayOrientation
}

// SetDisplayOrientation sets the display orientation in degrees.
// Valid values are 0, 90, 180, and 270 degrees.
func (c *Config) SetDisplayOrientation(orientation int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Normalize the orientation to valid values (0, 90, 180, 270)
	orientation %= 360
	if orientation < 0 {
		orientation += 360
	}

	// Round to nearest 90-degree increment
	orientation = (orientation + 45) / 90 * 90
	if orientation == 360 {
		orientation = 0
	}

	c.viper.Hardware.DisplayOrientation = orientation
	c.rebuildSnapshot()
	c.registerUpdate(false)
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

// SetDynamicTransmissionFeedbackEnabled sets whether dynamic transmission feedback is enabled.
func (c *Config) SetDynamicTransmissionFeedbackEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.DynamicTransmissionFeedback = value

	c.registerUpdate(false)
}

// GetJerkCurve returns the jerk curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) GetJerkCurve() float64 {
	return c.snapshot.Load().JerkCurve
}

// SetJerkCurve sets the jerk curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) SetJerkCurve(value int) {
	value = min(value, 955)
	value = max(value, 5)

	c.mu.Lock()
	c.viper.Haptics.JerkCurve = value
	c.mu.Unlock()
	c.updateJerkScale()
}

// IncreaseJerkCurve increases the jerk curve value in increments of 5.
func (c *Config) IncreaseJerkCurve() int {
	c.mu.Lock()
	c.viper.Haptics.JerkCurve = min(955, c.viper.Haptics.JerkCurve+5)
	result := c.viper.Haptics.JerkCurve
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// DecreaseJerkCurve decreases the jerk curve value in increments of 5.
func (c *Config) DecreaseJerkCurve() int {
	c.mu.Lock()
	c.viper.Haptics.JerkCurve = max(5, c.viper.Haptics.JerkCurve-5)
	result := c.viper.Haptics.JerkCurve
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// GetJerkScale returns the current jerk scale factor.
func (c *Config) GetJerkScale() float64 {
	return c.snapshot.Load().JerkScale
}

// GetJerkMax returns the maximum jerk value.
// The jerk curve is applied over the range from 0 to this maximum value.
// Any jerk vakues above this value are clamped to this maximum.
func (c *Config) GetJerkMax() int {
	return c.snapshot.Load().JerkMax
}

// SetJerkMax sets the maximum jerk value.
// The jerk curve is applied over the range from 0 to this maximum value.
// Any jerk vakues above this value are clamped to this maximum.
func (c *Config) SetJerkMax(value int) {
	value = min(value, 200)
	value = max(value, 1)

	c.mu.Lock()
	c.viper.Haptics.JerkMax = value
	c.mu.Unlock()
	c.updateJerkScale()
}

// IncreaseJerkMax increases the maximum jerk value in increments of 1.
func (c *Config) IncreaseJerkMax() int {
	c.mu.Lock()
	c.viper.Haptics.JerkMax = min(100, c.viper.Haptics.JerkMax+1)
	result := c.viper.Haptics.JerkMax
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// DecreaseJerkMax decreases the maximum jerk value in increments of 1.
func (c *Config) DecreaseJerkMax() int {
	c.mu.Lock()
	c.viper.Haptics.JerkMax = max(1, c.viper.Haptics.JerkMax-1)
	result := c.viper.Haptics.JerkMax
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// GetSnapCurve returns the snap curve value.
func (c *Config) GetSnapCurve() float64 {
	return c.snapshot.Load().SnapCurve
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

	c.registerUpdate(false)
}

// IncreaseSnapCurve increases the snap curve value in increments of 5.
func (c *Config) IncreaseSnapCurve() int {
	c.mu.Lock()

	c.viper.Haptics.SnapCurve = min(
		955,
		c.viper.Haptics.SnapCurve+5,
	)

	c.mu.Unlock()

	c.updateSnapScale()

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

	c.updateSnapScale()

	return c.viper.Haptics.SnapCurve
}

// GetSnapScale returns the current snap scale factor.
func (c *Config) GetSnapScale() float64 {
	return c.snapshot.Load().SnapScale
}

// GetSnapMax returns the maximum snap value.
func (c *Config) GetSnapMax() int {
	return c.snapshot.Load().SnapMax
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

	c.registerUpdate(false)
}

// IncreaseSnapMax increases the maximum snap value in increments of 1.
func (c *Config) IncreaseSnapMax() int {
	c.mu.Lock()

	c.viper.Haptics.SnapMax = min(
		100,
		c.viper.Haptics.SnapMax+1,
	)

	c.mu.Unlock()

	c.updateSnapScale()

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

	c.updateSnapScale()

	return c.viper.Haptics.SnapMax
}

// GetTransmissionCurve returns the transmission curve value.
// This curve is applied to the dynamic transmission feedback bsaed on the longitudinal vehicle g-force.
func (c *Config) GetTransmissionCurve() float64 {
	return float64(c.snapshot.Load().DynamicTransmissionCurve)
}

// SetTransmissionCurve sets the transmission curve value.
// This curve is applied to the dynamic transmission feedback bsaed on the longitudinal vehicle g-force.
func (c *Config) SetTransmissionCurve(value int) {
	c.mu.Lock()

	value = min(value, 955)
	value = max(value, 5)
	c.viper.Haptics.DynamicTransmissionCurve = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseTransmissionCurve increases the transmission curve value in increments of 5.
func (c *Config) IncreaseTransmissionCurve() int {
	c.mu.Lock()
	c.viper.Haptics.DynamicTransmissionCurve = min(955, c.viper.Haptics.DynamicTransmissionCurve+5)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Haptics.DynamicTransmissionCurve
	c.mu.Unlock()

	return result
}

// DecreaseTransmissionCurve decreases the transmission curve value in increments of 5.
func (c *Config) DecreaseTransmissionCurve() int {
	c.mu.Lock()
	c.viper.Haptics.DynamicTransmissionCurve = max(5, c.viper.Haptics.DynamicTransmissionCurve-5)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Haptics.DynamicTransmissionCurve
	c.mu.Unlock()

	return result
}

// GetTransmissionGforceMax returns the maximum g-force for dynamic transmission feedback.
// Any longitudinal g-force values above this are clamped to this maximum.
func (c *Config) GetTransmissionGforceMax() float64 {
	return c.snapshot.Load().DynamicTransmissionGforceMax
}

// SetTransmissionGforceMax sets the maximum transmission G-force value.
// Any longitudinal g-force values above this are clamped to this maximum.
func (c *Config) SetTransmissionGforceMax(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = math.Min(10, value)
	value = math.Max(0, value)

	c.viper.Haptics.DynamicTransmissionGforceMax = value

	c.registerUpdate(false)
}

// IncreaseTransmissionGforceMax increases the maximum g-force for dynamic transmission feedback in increments of 0.1g.
func (c *Config) IncreaseTransmissionGforceMax() float64 {
	c.mu.Lock()

	c.viper.Haptics.DynamicTransmissionGforceMax = min(
		5.0,
		c.viper.Haptics.DynamicTransmissionGforceMax+0.1,
	)

	c.mu.Unlock()

	c.registerUpdate(false)

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

	c.registerUpdate(false)

	return c.viper.Haptics.DynamicTransmissionGforceMax
}

// GetMinHz returns the configured minimum pulse frequency in Hz.
// This is the minimum frequency output for chassis bump haptics.
func (c *Config) GetMinHz() float64 {
	return c.snapshot.Load().PulseMinFrequencyHz
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

	c.registerUpdate(false)

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

	c.registerUpdate(false)

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

	c.registerUpdate(false)

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

	c.registerUpdate(false)

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

	c.registerUpdate(false)

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

	c.registerUpdate(false)

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

	c.registerUpdate(false)

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

	c.registerUpdate(false)

	return c.viper.Haptics._engineProfile.PulseScale
}

// IncreaseMinHz increases the minimum pulse frequency in 1 Hz increments.
// This is the minimum frequency output for chassis bump haptics and is clamped to a maximum of 25Hz.
func (c *Config) IncreaseMinHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMinFrequencyHz = min(25, c.viper.Haptics.PulseMinFrequencyHz+1)
	result := int(c.viper.Haptics.PulseMinFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// DecreaseMinHz decreases the minimum pulse frequency in 1 Hz increments.
// This is the minimum frequency output for chassis bump haptics and is clamped to a minimum of 5Hz.
func (c *Config) DecreaseMinHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMinFrequencyHz = max(5, c.viper.Haptics.PulseMinFrequencyHz-1)
	result := int(c.viper.Haptics.PulseMinFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// GetMaxHz returns the configured maximum pulse frequency in Hz.
// This is the maximum frequency output for chassis bump haptics.
func (c *Config) GetMaxHz() float64 {
	return c.snapshot.Load().PulseMaxFrequencyHz
}

// IncreaseMaxHz increases the maximum pulse frequency in 1 Hz increments.
// This is the maximum frequency output for chassis bump haptics and is clamped to a maximum of 100Hz.
func (c *Config) IncreaseMaxHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxFrequencyHz = min(100, c.viper.Haptics.PulseMaxFrequencyHz+1)
	result := int(c.viper.Haptics.PulseMaxFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// DecreaseMaxHz decreases the maximum pulse frequency in 1 Hz increments.
// This is the maximum frequency output for chassis bump haptics and is clamped to a minimum of 26Hz.
func (c *Config) DecreaseMaxHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxFrequencyHz = max(26, c.viper.Haptics.PulseMaxFrequencyHz-1)
	result := int(c.viper.Haptics.PulseMaxFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// GetFrequencyHzRange returns the range between the configured minimum and maximum pulse frequencies in Hz.
// This is the frequency range output for chassis bump haptics.
func (c *Config) GetFrequencyHzRange() float64 {
	snap := c.snapshot.Load()

	return snap.PulseMaxFrequencyHz - snap.PulseMinFrequencyHz
}

// GetPulseWidthMin returns the minimum pulse width in samples based on the current max frequency.
func (c *Config) GetPulseWidthMin() float64 {
	return c.snapshot.Load().PulseWidthMin
}

// GetPulseWidthMax returns the maximum pulse width in samples based on the current min and max frequencies.
func (c *Config) GetPulseWidthMax() float64 {
	return c.snapshot.Load().PulseWidthMax
}

// GetPulseMaxAmplitude returns the maximum pulse amplitude for chassis bump haptics.
func (c *Config) GetPulseMaxAmplitude() float64 {
	return c.snapshot.Load().PulseMaxAmplitude
}

// SetPulseMaxAmplitude sets the maximum pulse amplitude for chassis bump haptics.
func (c *Config) SetPulseMaxAmplitude(value float64) {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxAmplitude = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetPulseMaxFrequencyHz returns the maximum pulse frequency in Hz for chassis bump haptics.
func (c *Config) GetPulseMaxFrequencyHz() float64 {
	return c.snapshot.Load().PulseMaxFrequencyHz
}

// SetPulseMaxFrequencyHz sets the maximum pulse frequency in Hz for chassis bump haptics.
func (c *Config) SetPulseMaxFrequencyHz(value float64) {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxFrequencyHz = value
	c.updatePulseWidthExtents()
	c.mu.Unlock()
}

// SetPulseMinFrequencyHz sets the minimum pulse frequency in Hz for chassis bump haptics.
func (c *Config) SetPulseMinFrequencyHz(value float64) {
	c.mu.Lock()
	c.viper.Haptics.PulseMinFrequencyHz = value
	c.updatePulseWidthExtents()
	c.mu.Unlock()
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

// SetPitRadioEnabled sets whether pit radio integration is enabled.
func (c *Config) SetPitRadioEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Enabled = value

	c.registerUpdate(true)
}

// GetMessageSendIntervalMs returns the interval in milliseconds between sending of pit radio messages.
func (c *Config) GetMessageSendIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.MessageSendIntervalMs
}

// SetMessageSendIntervalMs sets the interval in milliseconds between sending of pit radio messages.
func (c *Config) SetMessageSendIntervalMs(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.MessageSendIntervalMs = value

	c.registerUpdate(false)
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
	return c.snapshot.Load().InternalSampleRateHz
}

// GetOutputSampleRateHz returns the output sample rate of the synthesizer in Hz.
// This is the sample rate at which audio is output to the audio device or file.
// 32000 Hz is suitable for most common hardware but some may work at lower rates.
func (c *Config) GetOutputSampleRateHz() int {
	return c.snapshot.Load().OutputSampleRateHz
}

// SetInternalSampleRateHz sets the internal sample rate of the synthesizer in Hz.
func (c *Config) SetInternalSampleRateHz(value int) {
	c.mu.Lock()
	c.viper.Synthesizer.InternalSampleRateHz = value
	c.updatePulseWidthExtents()
	c.rebuildSnapshot()
	c.registerUpdate(true)
	c.mu.Unlock()
}

// SetOutputSampleRateHz sets the output sample rate of the synthesizer in Hz.
func (c *Config) SetOutputSampleRateHz(value int) {
	c.mu.Lock()
	c.viper.Synthesizer.OutputSampleRateHz = value
	c.rebuildSnapshot()
	c.registerUpdate(true)
	c.mu.Unlock()
}

// GetGainIncrement returns the gain increment value.
func (c *Config) GetGainIncrement() float64 {
	return c.snapshot.Load().GainIncrement
}

// SetGainIncrement sets the gain increment value.
func (c *Config) SetGainIncrement(value float64) {
	c.mu.Lock()

	value = max(0.01, value)
	value = min(10, value)
	c.viper.Synthesizer.GainIncrement = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetMasterGain returns the master gain of the synthesizer (i.e. the overall volume level).
// This is a global gain applied to all haptic feedback.
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) GetMasterGain() float64 {
	return c.snapshot.Load().MasterGain
}

// SetMasterGain sets the master gain of the synthesizer.
// This is a global gain applied to all haptic feedback.
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) SetMasterGain(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.MasterGain = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetMasterGainMute returns whether the master gain is muted.
func (c *Config) GetMasterGainMute() bool {
	return c.snapshot.Load().MasterGainMute
}

// SetMasterGainMute sets whether the master gain is muted.
func (c *Config) SetMasterGainMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.MasterGainMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseMasterGain increases the master gain by the configured gain increment.
func (c *Config) IncreaseMasterGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.MasterGain = min(
		MaximumGain,
		c.viper.Synthesizer.MasterGain+c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.MasterGain
	c.mu.Unlock()

	return result
}

// DecreaseMasterGain decreases the master gain by the configured gain increment.
func (c *Config) DecreaseMasterGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.MasterGain = max(
		MinimumGain,
		c.viper.Synthesizer.MasterGain-c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.MasterGain
	c.mu.Unlock()

	return result
}

// GetChassisGain returns the chassis gain of the synthesizer (i.e. the volume level for chassis bump haptics).
// 0.0 is maximum gain and -60.0 will mute chassis bump haptic output.
func (c *Config) GetChassisGain() float64 {
	return c.snapshot.Load().ChassisGain
}

// GetChassisGainMute returns whether the chassis gain is muted.
func (c *Config) GetChassisGainMute() bool {
	return c.snapshot.Load().ChassisGainMute
}

// SetChassisGainMute sets whether the chassis gain is muted.
func (c *Config) SetChassisGainMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.ChassisGainMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetChassisGain sets the chassis gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute chassis bump haptic output.
func (c *Config) SetChassisGain(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.ChassisGain = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseChassisGain increases the chassis gain by the configured gain increment.
func (c *Config) IncreaseChassisGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.ChassisGain = min(
		MaximumGain,
		c.viper.Synthesizer.ChassisGain+c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.ChassisGain
	c.mu.Unlock()

	return result
}

// DecreaseChassisGain decreases the chassis gain by the configured gain increment.
func (c *Config) DecreaseChassisGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.ChassisGain = max(
		MinimumGain,
		c.viper.Synthesizer.ChassisGain-c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.ChassisGain
	c.mu.Unlock()

	return result
}

// SetTransmissionGainMinRace sets the minimum transmission gain for race transmissions.
func (c *Config) SetTransmissionGainMinRace(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinRace = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetTransmissionGainMinStreet sets the minimum transmission gain for street transmissions.
func (c *Config) SetTransmissionGainMinStreet(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinStreet = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetTransmissionGain returns the transmission gain of the synthesizer (i.e. the volume level for transmission
// haptics).
// 0.0 is maximum gain and -60.0 will mute transmission haptic output.
func (c *Config) GetTransmissionGain() float64 {
	return c.snapshot.Load().TransmissionGain
}

// GetTransmissionGainMute returns whether the transmission gain is muted.
func (c *Config) GetTransmissionGainMute() bool {
	return c.snapshot.Load().TransmissionGainMute
}

// SetTransmissionGainMute sets whether the transmission gain is muted.
func (c *Config) SetTransmissionGainMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetTransmissionGain sets the transmission gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute transmission haptic output.
func (c *Config) SetTransmissionGain(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGain = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseTransmissionGain increases the transmission gain by the configured gain increment.
func (c *Config) IncreaseTransmissionGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGain = min(
		MaximumGain,
		c.viper.Synthesizer.TransmissionGain+c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TransmissionGain
	c.mu.Unlock()

	return result
}

// DecreaseTransmissionGain decreases the transmission gain by the configured gain increment.
func (c *Config) DecreaseTransmissionGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGain = max(
		MinimumGain,
		c.viper.Synthesizer.TransmissionGain-c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TransmissionGain
	c.mu.Unlock()

	return result
}

// GetTransmissionGainMinRace returns the minimum transmission gain for race vehicle types.
// This is the transnmission haptic feebdack volume applied when the vehicle is stationary.
func (c *Config) GetTransmissionGainMinRace() float64 {
	return c.snapshot.Load().TransmissionGainMinRace
}

// GetTransmissionGainMinStreet returns the minimum transmission gain for street vehicle types.
// This is the transmission haptic feedback volume applied when the vehicle is stationary.
func (c *Config) GetTransmissionGainMinStreet() float64 {
	return c.snapshot.Load().TransmissionGainMinStreet
}

// GetEngineGain returns the gain fir the currently selected engine (i.e. the volume level for engine haptics).
// 0.0 is maximum gain and -60.0 will mute engine haptic output.
func (c *Config) GetEngineGain() float64 {
	return c.snapshot.Load().EngineGain
}

// GetEngineGainMute returns whether the engine gain is muted.
func (c *Config) GetEngineGainMute() bool {
	return c.snapshot.Load().EngineGainMute
}

// SetEngineGainMute sets whether the engine gain is muted.
func (c *Config) SetEngineGainMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.EngineGainMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetEngineGain sets the engine gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute engine haptic output.
func (c *Config) SetEngineGain(gain float64) {
	c.mu.Lock()
	c.viper.Synthesizer.EngineGain = max(MinimumGain, min(MaximumGain, gain))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseEngineGain increases the gain of the currently selected engine by the configured gain increment.
func (c *Config) IncreaseEngineGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.EngineGain = min(
		MaximumGain,
		c.viper.Synthesizer.EngineGain+c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.EngineGain
	c.mu.Unlock()

	return result
}

// DecreaseEngineGain decreases the gain of the currently selected engine by the configured gain increment.
func (c *Config) DecreaseEngineGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.EngineGain = max(
		MinimumGain,
		c.viper.Synthesizer.EngineGain-c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.EngineGain
	c.mu.Unlock()

	return result
}

// GetOutputFile returns the synthesizer output file path.
func (c *Config) GetOutputFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.OutputFile
}

// GetEngineProfiles returns all engine profiles as a map.
func (c *Config) GetEngineProfiles() map[string]appHaptics.EngineProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.EngineProfiles
}

// SetEngineProfile updates or creates an engine profile.
func (c *Config) SetEngineProfile(name string, profile appHaptics.EngineProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = strings.ToLower(name)
	c.viper.Haptics.EngineProfiles[name] = profile

	c.registerUpdate(false)
}

// GetEqEnabled returns whether the equalizer is enabled.
func (c *Config) GetEqEnabled() bool {
	return c.snapshot.Load().EqEnabled
}

// SetEqEnabled sets whether the equalizer is enabled.
func (c *Config) SetEqEnabled(enabled bool) {
	c.mu.Lock()
	c.viper.Synthesizer.EqEnabled = enabled
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetEq returns the equalizer bands.
func (c *Config) GetEq() []EQBand {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.EqBands
}

// GetEqCurve returns the computed EQ curve for fast lookup.
// Returns the curve, minimum frequency, and resolution (Hz per bucket).
func (c *Config) GetEqCurve() ([]float64, float64, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer._eqCurve,
		c.viper.Synthesizer._eqMinFreq,
		c.viper.Synthesizer._eqResolution
}

// SetEq sets the equalizer bands and recomputes the curve.
func (c *Config) SetEq(bands []EQBand) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(bands) == 8 {
		c.viper.Synthesizer.EqBands = bands
		c.computeEqCurve()
		c.registerUpdate(false)
	}
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

	c.registerUpdate(true)
}

// ****************************************************************************
// Tyres section methods.
// ****************************************************************************

// GetTyreMonitoringEnabled returns whether tyre monitoring is enabled.
func (c *Config) GetTyreMonitoringEnabled() bool {
	return c.snapshot.Load().TyreMonitoringEnabled
}

// SetTyreMonitoringEnabled sets whether tyre monitoring is enabled.
func (c *Config) SetTyreMonitoringEnabled(value bool) {
	c.mu.Lock()
	c.viper.Tyres.MonitoringEnabled = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetTyreTemperatureOptimalCelsius returns the optimal (center) tyre temperature in Celsius.
func (c *Config) GetTyreTemperatureOptimalCelsius() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Tyres.TemperatureOptimalCelsius
}

// SetTyreTemperatureOptimalCelsius sets the optimal (center) tyre temperature in Celsius.
func (c *Config) SetTyreTemperatureOptimalCelsius(value float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Tyres.TemperatureOptimalCelsius = value

	c.registerUpdate(false)
}

// GetTyreTemperatureOperatingWindow returns the total operating window width around optimal temperature in Celsius.
// The ideal temperature range is calculated as optimal ± (window/2).
func (c *Config) GetTyreTemperatureOperatingWindow() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Tyres.TemperatureOperatingWindow
}

// SetTyreTemperatureOperatingWindow sets the total operating window width around optimal temperature in Celsius.
func (c *Config) SetTyreTemperatureOperatingWindow(value float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Tyres.TemperatureOperatingWindow = value

	c.registerUpdate(false)
}

// GetTyreTemperatureMarginCelsius returns the margin beyond operating window for hot/cold thresholds in Celsius.
func (c *Config) GetTyreTemperatureMarginCelsius() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Tyres.TemperatureMarginCelsius
}

// SetTyreTemperatureMarginCelsius sets the margin beyond operating window for hot/cold thresholds in Celsius.
func (c *Config) SetTyreTemperatureMarginCelsius(value float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Tyres.TemperatureMarginCelsius = value

	c.registerUpdate(false)
}

// ****************************************************************************
// Configuration file management methods.
// ****************************************************************************

// BackupConfigFile creates a backup of the current configuration file.
// Returns the backup filename and any error encountered.
func (c *Config) BackupConfigFile() (string, error) {
	// If the file doesn't exist, nothing to back up
	_, err := os.Stat(c.configFile)
	if err != nil {
		return "", fmt.Errorf("configuration file %s not found", c.configFile)
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup.%s", c.configFile, timestamp)

	source, err := os.Open(c.configFile)
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
// Skips writing if the config hasn't changed to reduce disk wear.
func (c *Config) SaveConfigToFile() error {
	// If no config file was specified, we can't save
	if c.configFile == "" {
		return errors.New("no config file specified")
	}

	// Lock for the entire save operation to serialize concurrent saves
	c.mu.Lock()
	defer c.mu.Unlock()

	// Marshal the configuration to TOML
	tomlData, err := toml.Marshal(c.viper)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration to TOML: %w", err)
	}

	// Skip write if config hasn't changed (reduces SD card wear)
	if bytes.Equal(tomlData, c.lastSavedConfig) {
		return nil
	}

	// Ensure the directory exists
	configDir := filepath.Dir(c.configFile)

	err = os.MkdirAll(configDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", configDir, err)
	}

	// Write directly to config file with fsync to ensure durability
	file, err := os.OpenFile(c.configFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open config file %s: %w", c.configFile, err)
	}

	_, err = file.Write(tomlData)
	if err != nil {
		file.Close()

		return fmt.Errorf("failed to write config file %s: %w", c.configFile, err)
	}

	// Force sync to disk before closing
	err = file.Sync()
	if err != nil {
		file.Close()

		return fmt.Errorf("failed to sync config file %s: %w", c.configFile, err)
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("failed to close config file %s: %w", c.configFile, err)
	}

	// Update last saved config
	c.lastSavedConfig = tomlData

	return nil
}

// ****************************************************************************
// Public Helper methods.
// ****************************************************************************

// GetConfigFilePath returns the path to the configuration file.
func (c *Config) GetConfigFilePath() string {
	return c.configFile
}

// ****************************************************************************
// Private Helper methods.
// ****************************************************************************

// finalise performs validation of the config and updates any derived configuration values.
func (c *Config) finalise() {
	c.mu.Lock()

	if len(c.viper.Synthesizer.EqBands) != 8 {
		log.Warn().Int("length", len(c.viper.Synthesizer.EqBands)).Msg("invalid synthesizer EQ bands length")

		// Initialize with 8 default bands spanning 10-60 Hz
		c.viper.Synthesizer.EqBands = []EQBand{
			{Frequency: 12, Gain: 0.0},
			{Frequency: 16, Gain: 0.0},
			{Frequency: 20, Gain: 0.0},
			{Frequency: 25, Gain: 0.0},
			{Frequency: 30, Gain: 0.0},
			{Frequency: 38, Gain: 0.0},
			{Frequency: 48, Gain: 0.0},
			{Frequency: 58, Gain: 0.0},
		}
	}

	// Compute the EQ curve for efficient runtime application
	c.computeEqCurve()

	// Update pulse width extents (inline since we hold the lock)
	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMaxFrequencyHz)
	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMinFrequencyHz)

	c.mu.Unlock()

	c.updateJerkScale()
	c.updateSnapScale()
}

// updateJerkScale recalculates the jerk scale factor based on the current jerk curve, scale and maximum.
func (c *Config) updateJerkScale() {
	c.mu.Lock()
	exponent := float64(c.viper.Haptics.JerkCurve) / 1000.0
	jerkMax := 100 * float64(c.viper.Haptics.JerkMax)
	c.viper.Haptics._jerkScale = 1 / math.Pow(jerkMax, exponent)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// updateSnapScale recalculates the snap scale factor based on the current snap curve, scale and maximum.
func (c *Config) updateSnapScale() {
	c.mu.Lock()
	exponent := float64(c.viper.Haptics.SnapCurve) / 1000.0
	snapMax := 1000 * float64(c.viper.Haptics.SnapMax)
	c.viper.Haptics._snapScale = 1 / math.Pow(snapMax, exponent)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// computeEqCurve computes the EQ curve based on the current bands.
// Uses 8-band parametric EQ with bell filters.
func (c *Config) computeEqCurve() {
	const minFreq = 10.0 // Minimum frequency in Hz

	const maxFreq = 70.0 // Maximum frequency in Hz

	const resolution = 0.5 // Hz per bucket

	numBuckets := int((maxFreq-minFreq)/resolution) + 1
	curve := make([]float64, numBuckets)

	// For each frequency bucket, compute the EQ response using bell filter
	for bucketNum := range numBuckets {
		freq := minFreq + float64(bucketNum)*resolution

		// Start with unity gain (1.0 in linear, 0.0 in dB)
		amplitudeRatio := 1.0

		// Apply each band's bell filter by multiplication in linear space
		for _, band := range c.viper.Synthesizer.EqBands {
			// Calculate bell filter response at this frequency
			// Using per-band Q factor for bandwidth control
			if band.Gain != 0.0 {
				// Use band's qVal value, default to 2.0 if not set
				qVal := band.Q
				if qVal <= 0 {
					qVal = 2.0
				}

				freqRatio := freq / band.Frequency
				if freqRatio > 0 {
					// Bell filter magnitude response in dB
					// H(f) = G / sqrt(1 + Q^2 * (f/fc - fc/f)^2)
					// At center frequency (f = fc), delta = 0, denom = 1, so gain = G (exact)
					delta := freqRatio - 1.0/freqRatio
					denom := math.Sqrt(1.0 + qVal*qVal*delta*delta)

					if denom > 0 {
						// Calculate this band's gain at this frequency in dB
						bandGainDB := band.Gain / denom
						// Convert to amplitude ratio and multiply
						amplitudeRatio *= math.Pow(10, bandGainDB/20)
					}
				}
			}
		}

		// Store the final amplitude ratio for efficient multiplication
		curve[bucketNum] = amplitudeRatio
	}

	c.viper.Synthesizer._eqCurve = curve
	c.viper.Synthesizer._eqMinFreq = minFreq
	c.viper.Synthesizer._eqMaxFreq = maxFreq
	c.viper.Synthesizer._eqResolution = resolution
}

// registerUpdate records the time of the last configuration update.
// Assumes that the caller holds the write lock.
func (c *Config) registerUpdate(restartRequired bool) {
	c.status.LastUpdate = time.Now().Unix()

	if restartRequired {
		c.status.RestartRequired = restartRequired
	}
}

// rebuildSnapshot creates a new immutable snapshot for lock-free reads.
// Assumes that the caller holds the write lock.
func (c *Config) rebuildSnapshot() {
	newSnap := &ConfigSnapshot{
		MasterGain:                c.viper.Synthesizer.MasterGain,
		MasterGainMute:            c.viper.Synthesizer.MasterGainMute,
		ChassisGain:               c.viper.Synthesizer.ChassisGain,
		ChassisGainMute:           c.viper.Synthesizer.ChassisGainMute,
		TransmissionGain:          c.viper.Synthesizer.TransmissionGain,
		TransmissionGainMute:      c.viper.Synthesizer.TransmissionGainMute,
		TransmissionGainMinRace:   c.viper.Synthesizer.TransmissionGainMinRace,
		TransmissionGainMinStreet: c.viper.Synthesizer.TransmissionGainMinStreet,
		EngineGain:                c.viper.Synthesizer.EngineGain,
		EngineGainMute:            c.viper.Synthesizer.EngineGainMute,
		GainIncrement:             c.viper.Synthesizer.GainIncrement,
		InternalSampleRateHz:      c.viper.Synthesizer.InternalSampleRateHz,
		OutputSampleRateHz:        c.viper.Synthesizer.OutputSampleRateHz,

		JerkCurve: float64(c.viper.Haptics.JerkCurve),
		JerkMax:   c.viper.Haptics.JerkMax,
		JerkScale: c.viper.Haptics._jerkScale,

		SnapCurve: float64(c.viper.Haptics.SnapCurve),
		SnapMax:   c.viper.Haptics.SnapMax,
		SnapScale: c.viper.Haptics._snapScale,

		PulseMaxAmplitude:   c.viper.Haptics.PulseMaxAmplitude,
		PulseMaxFrequencyHz: c.viper.Haptics.PulseMaxFrequencyHz,
		PulseMinFrequencyHz: c.viper.Haptics.PulseMinFrequencyHz,
		PulseWidthMin:       c.viper.Haptics._pulseWidthMin,
		PulseWidthMax:       c.viper.Haptics._pulseWidthMax,

		DynamicTransmissionFeedback:  c.viper.Haptics.DynamicTransmissionFeedback,
		DynamicTransmissionCurve:     c.viper.Haptics.DynamicTransmissionCurve,
		DynamicTransmissionGforceMax: c.viper.Haptics.DynamicTransmissionGforceMax,

		EqEnabled: c.viper.Synthesizer.EqEnabled,

		FuelMonitoringEnabled: c.viper.Fuel.MonitoringEnabled,
		TyreMonitoringEnabled: c.viper.Tyres.MonitoringEnabled,

		DisplayOrientation: c.viper.Hardware.DisplayOrientation,
	}
	c.snapshot.Store(newSnap)
}

// updatePulseWidthExtents recalculates the minimum and maximum pulse widths in samples.
// Assumes the caller does NOT hold the lock.
func (c *Config) updatePulseWidthExtents() {
	c.mu.Lock()
	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMaxFrequencyHz)

	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMinFrequencyHz)

	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}
