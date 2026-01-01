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
	WebUIEnabled  bool   `toml:"webUIEnabled"`
	WebUIPort     int    `toml:"webUIPort"`
}

type discord struct {
	Token          string `toml:"token"`
	GuildID        string `toml:"guildID"`
	ChannelID      string `toml:"channelID"`
	VoiceChannelID string `toml:"voiceChannelID"`
}

type fuelMonitoring struct {
	Enabled                 bool    `toml:"monitoringEnabled"`
	PreWarnNotifyLaps       float64 `toml:"preWarnNotifyLaps"`
	StrategyNotifyLaps      float64 `toml:"strategyNotifyLaps"`
	RangeSafetyMarginLaps   float64 `toml:"rangeSafetyMarginLaps"`
	RangeSafetyMarginMeters float64 `toml:"rangeSafetyMarginMeters"`
}

type haptics struct {
	EnableReplay                 bool                                `toml:"enableReplay"`
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

type notifications struct {
	RaceProgressEnabled     bool    `toml:"raceProgressEnabled"`
	RaceProgressMinLaps     int     `toml:"raceProgressMinLaps"`
	RaceProgressIntervalPc  int     `toml:"raceProgressIntervalPc"`
	RaceLapsEnabled         bool    `toml:"raceLapsEnabled"`
	RaceLapsIntervalLaps    int     `toml:"raceLapsIntervalLaps"`
	RaceLapsCountdownLaps   int     `toml:"raceLapsCountdownLaps"`
	LapTimesEnabled         bool    `toml:"lapTimesEnabled"`
	LapTimesMaxDeltaSeconds float64 `toml:"lapTimesMaxDeltaSeconds"`
	CircuitMatchingEnabled  bool    `toml:"circuitMatchingEnabled"`
}

type pitRadio struct {
	Enabled               bool            `toml:"enabled"`
	MessageSendIntervalMs int             `toml:"messageSendIntervalMs"`
	Notifications         *notifications  `toml:"notifications"`
	Discord               *discord        `toml:"discord"`
	FuelMonitoring        *fuelMonitoring `toml:"fuelMonitoring"`
	TyreMonitoring        *tyreMonitoring `toml:"tyreMonitoring"`
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
	MasterMute                bool      `toml:"masterMute"`
	MasterGain                float64   `toml:"masterGain"`
	ChassisMute               bool      `toml:"chassisMute"`
	ChassisGain               float64   `toml:"chassisGain"`
	TransmissionMute          bool      `toml:"transmissionMute"`
	TransmissionGain          float64   `toml:"transmissionGain"`
	TransmissionGainMinRace   float64   `toml:"transmissionGainMinRace"`
	TransmissionGainMinStreet float64   `toml:"transmissionGainMinStreet"`
	EngineMute                bool      `toml:"engineMute"`
	EngineGain                float64   `toml:"engineGain"`
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

type tyreMonitoring struct {
	Enabled                    bool    `toml:"enabled"`
	TemperatureOptimalCelsius  float32 `toml:"temperatureOptimalCelsius"`
	TemperatureOperatingWindow float32 `toml:"temperatureOperatingWindow"`
	TemperatureMarginCelsius   float32 `toml:"temperatureMarginCelsius"`
}

type viperConfig struct {
	App         *app         `toml:"app"`
	Hardware    *hardware    `toml:"hardware"`
	Haptics     *haptics     `toml:"haptics"`
	PitRadio    *pitRadio    `toml:"pitRadio"`
	Synthesizer *Synthesizer `toml:"synthesizer"`
	Telemetry   *Telemetry   `toml:"telemetry"`
}

// Snapshot holds frequently-accessed configuration values for lock-free reads.
type Snapshot struct {
	// Synthesizer gain settings
	MasterMute                bool
	MasterGain                float64
	ChassisMute               bool
	ChassisGain               float64
	TransmissionMute          bool
	TransmissionGain          float64
	TransmissionGainMinRace   float64
	TransmissionGainMinStreet float64
	EngineMute                bool
	EngineGain                float64
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
	snapshot        atomic.Pointer[Snapshot]
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
	_, err := os.Stat(defaultConfigPath)
	if err == nil {
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

// GethapticsDynamicTransFeedbackEnabled returns true if dynamic transmission feedback is enabled.
func (c *Config) GethapticsDynamicTransFeedbackEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.DynamicTransmissionFeedback
}

// SetHapticsDynamicTransFeedbackEnabled sets whether dynamic transmission feedback is enabled.
func (c *Config) SetHapticsDynamicTransFeedbackEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.DynamicTransmissionFeedback = value

	c.registerUpdate(false)
}

// GethapticsJerkCurve returns the jerk curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) GethapticsJerkCurve() float64 {
	return c.snapshot.Load().JerkCurve
}

// SetHapticsJerkCurve sets the jerk curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) SetHapticsJerkCurve(value int) {
	value = min(value, 955)
	value = max(value, 5)

	c.mu.Lock()
	c.viper.Haptics.JerkCurve = value
	c.mu.Unlock()
	c.updateJerkScale()
}

// IncreaseHapticsJerkCurve increases the jerk curve value in increments of 5.
func (c *Config) IncreaseHapticsJerkCurve() int {
	c.mu.Lock()
	c.viper.Haptics.JerkCurve = min(955, c.viper.Haptics.JerkCurve+5)
	result := c.viper.Haptics.JerkCurve
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// DecreaseHapticsJerkCurve decreases the jerk curve value in increments of 5.
func (c *Config) DecreaseHapticsJerkCurve() int {
	c.mu.Lock()
	c.viper.Haptics.JerkCurve = max(5, c.viper.Haptics.JerkCurve-5)
	result := c.viper.Haptics.JerkCurve
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// GetHapticsJerkScale returns the current jerk scale factor.
func (c *Config) GetHapticsJerkScale() float64 {
	return c.snapshot.Load().JerkScale
}

// GetHapticsJerkMax returns the maximum jerk value.
// The jerk curve is applied over the range from 0 to this maximum value.
// Any jerk vakues above this value are clamped to this maximum.
func (c *Config) GetHapticsJerkMax() int {
	return c.snapshot.Load().JerkMax
}

// SetHapticsJerkMax sets the maximum jerk value.
// The jerk curve is applied over the range from 0 to this maximum value.
// Any jerk vakues above this value are clamped to this maximum.
func (c *Config) SetHapticsJerkMax(value int) {
	value = min(value, 200)
	value = max(value, 1)

	c.mu.Lock()
	c.viper.Haptics.JerkMax = value
	c.mu.Unlock()
	c.updateJerkScale()
}

// IncreaseHapticsJerkMax increases the maximum jerk value in increments of 1.
func (c *Config) IncreaseHapticsJerkMax() int {
	c.mu.Lock()
	c.viper.Haptics.JerkMax = min(100, c.viper.Haptics.JerkMax+1)
	result := c.viper.Haptics.JerkMax
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// DecreaseHapticsJerkMax decreases the maximum jerk value in increments of 1.
func (c *Config) DecreaseHapticsJerkMax() int {
	c.mu.Lock()
	c.viper.Haptics.JerkMax = max(1, c.viper.Haptics.JerkMax-1)
	result := c.viper.Haptics.JerkMax
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// GetHapticsEnableReplay returns true if replay mode is enabled.
func (c *Config) GetHapticsEnableReplay() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.EnableReplay
}

// SetHapticsEnableReplay sets whether haptics are generated for replays.
func (c *Config) SetHapticsEnableReplay(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.EnableReplay = value

	c.registerUpdate(true)
}

// GetHapticsSnapCurve returns the snap curve value.
func (c *Config) GetHapticsSnapCurve() float64 {
	return c.snapshot.Load().SnapCurve
}

// SetHapticsSnapCurve sets the snap curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) SetHapticsSnapCurve(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = min(value, 955)
	value = max(value, 5)

	c.viper.Haptics.SnapCurve = value

	c.registerUpdate(false)
}

// IncreaseHapticsSnapCurve increases the snap curve value in increments of 5.
func (c *Config) IncreaseHapticsSnapCurve() int {
	c.mu.Lock()

	c.viper.Haptics.SnapCurve = min(
		955,
		c.viper.Haptics.SnapCurve+5,
	)

	c.mu.Unlock()

	c.updateSnapScale()

	return c.viper.Haptics.SnapCurve
}

// DecreaseHapticsSnapCurve decreases the snap curve value in increments of 5.
func (c *Config) DecreaseHapticsSnapCurve() int {
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

// GetHapticsSnapScale returns the current snap scale factor.
func (c *Config) GetHapticsSnapScale() float64 {
	return c.snapshot.Load().SnapScale
}

// GetHapticsSnapMax returns the maximum snap value.
func (c *Config) GetHapticsSnapMax() int {
	return c.snapshot.Load().SnapMax
}

// SetHapticsSnapMax sets the maximum snap value.
// The snap curve is applied over the range from 0 to this maximum value.
// Any snap values above this value are clamped to this maximum.
// Allowed range is 1 to 200.
func (c *Config) SetHapticsSnapMax(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = min(value, 200)
	value = max(value, 1)

	c.viper.Haptics.SnapMax = value

	c.registerUpdate(false)
}

// IncreaseHapticsSnapMax increases the maximum snap value in increments of 1.
func (c *Config) IncreaseHapticsSnapMax() int {
	c.mu.Lock()

	c.viper.Haptics.SnapMax = min(
		100,
		c.viper.Haptics.SnapMax+1,
	)

	c.mu.Unlock()

	c.updateSnapScale()

	return c.viper.Haptics.SnapMax
}

// DecreaseHapticsSnapMax decreases the maximum snap value in increments of 1.
func (c *Config) DecreaseHapticsSnapMax() int {
	c.mu.Lock()

	c.viper.Haptics.SnapMax = max(
		1,
		c.viper.Haptics.SnapMax-1,
	)

	c.mu.Unlock()

	c.updateSnapScale()

	return c.viper.Haptics.SnapMax
}

// GetHapticsTransmissionCurve returns the transmission curve value.
// This curve is applied to the dynamic transmission feedback bsaed on the longitudinal vehicle g-force.
func (c *Config) GetHapticsTransmissionCurve() float64 {
	return float64(c.snapshot.Load().DynamicTransmissionCurve)
}

// SetHapticsTransmissionCurve sets the transmission curve value.
// This curve is applied to the dynamic transmission feedback bsaed on the longitudinal vehicle g-force.
func (c *Config) SetHapticsTransmissionCurve(value int) {
	c.mu.Lock()

	value = min(value, 955)
	value = max(value, 5)
	c.viper.Haptics.DynamicTransmissionCurve = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseHapticsTransmissionCurve increases the transmission curve value in increments of 5.
func (c *Config) IncreaseHapticsTransmissionCurve() int {
	c.mu.Lock()
	c.viper.Haptics.DynamicTransmissionCurve = min(955, c.viper.Haptics.DynamicTransmissionCurve+5)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Haptics.DynamicTransmissionCurve
	c.mu.Unlock()

	return result
}

// DecreaseHapticsTransmissionCurve decreases the transmission curve value in increments of 5.
func (c *Config) DecreaseHapticsTransmissionCurve() int {
	c.mu.Lock()
	c.viper.Haptics.DynamicTransmissionCurve = max(5, c.viper.Haptics.DynamicTransmissionCurve-5)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Haptics.DynamicTransmissionCurve
	c.mu.Unlock()

	return result
}

// GetHapticsTransmissionGforceMax returns the maximum g-force for dynamic transmission feedback.
// Any longitudinal g-force values above this are clamped to this maximum.
func (c *Config) GetHapticsTransmissionGforceMax() float64 {
	return c.snapshot.Load().DynamicTransmissionGforceMax
}

// SetHapticsTransmissionGforceMax sets the maximum transmission G-force value.
// Any longitudinal g-force values above this are clamped to this maximum.
func (c *Config) SetHapticsTransmissionGforceMax(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value = math.Min(10, value)
	value = math.Max(0, value)

	c.viper.Haptics.DynamicTransmissionGforceMax = value

	c.registerUpdate(false)
}

// IncreaseHapticsTransmissionGforceMax increases the maximum g-force for dynamic transmission feedback in increments of 0.1g.
func (c *Config) IncreaseHapticsTransmissionGforceMax() float64 {
	c.mu.Lock()

	c.viper.Haptics.DynamicTransmissionGforceMax = min(
		5.0,
		c.viper.Haptics.DynamicTransmissionGforceMax+0.1,
	)

	c.mu.Unlock()

	c.registerUpdate(false)

	return c.viper.Haptics.DynamicTransmissionGforceMax
}

// DecreasehapticsTransmissionGforceMax decreases the maximum g-force for dynamic transmission feedback in increments of 0.1g.
func (c *Config) DecreasehapticsTransmissionGforceMax() float64 {
	c.mu.Lock()

	c.viper.Haptics.DynamicTransmissionGforceMax = max(
		0.1,
		c.viper.Haptics.DynamicTransmissionGforceMax-0.1,
	)

	c.mu.Unlock()

	c.registerUpdate(false)

	return c.viper.Haptics.DynamicTransmissionGforceMax
}

// GetHapticsPulseMinHz returns the configured minimum pulse frequency in Hz.
// This is the minimum frequency output for chassis bump haptics.
func (c *Config) GetHapticsPulseMinHz() float64 {
	return c.snapshot.Load().PulseMinFrequencyHz
}

// GetHapticsEngineProfile returns the currently selected engine profile.
// If no profile is selected, it returns nil.
func (c *Config) GetHapticsEngineProfile(name string) *appHaptics.EngineProfile {
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

// GetHapticesEnginePrimaryBalance returns the current engine primary balance.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) GetHapticesEnginePrimaryBalance() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	return c.viper.Haptics._engineProfile.PrimaryBalance
}

// IncreaseHapticsEnginePrimaryBalance increases the current engoine primary balancee in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) IncreaseHapticsEnginePrimaryBalance() float64 {
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

// DecreaseHapticsEnginePrimaryBalance decreases the current engine primary balance in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) DecreaseHapticsEnginePrimaryBalance() float64 {
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

// GetHapticsEngineSecondaryBalance returns the current engine secondary balance.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) GetHapticsEngineSecondaryBalance() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	return c.viper.Haptics._engineProfile.SecondaryBalance
}

// IncreaseHapticsEngineSecondaryBalance increases the current engine secondary balance in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) IncreaseHapticsEngineSecondaryBalance() float64 {
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

// DecreaseHapticsEngineSecondaryBalance decreases the current engine secondary balance in increments of 0.01.
// If no profile is selected, it returns 1.0 (perfect balance).
func (c *Config) DecreaseHapticsEngineSecondaryBalance() float64 {
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

// GetHapticsEnginePulseGain returns the current engine pulse gain (i.e. engine haptic volume).
// If no profile is selected, it returns a gain level that silences engine haptics.
func (c *Config) GetHapticsEnginePulseGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return MinimumGain
	}

	return c.viper.Haptics._engineProfile.Gain
}

// IncreaseHapticsEnginePulseGain increases the current engine pulse gain by the configured increment.
// If no profile is selected, it returns a gain level that silences engine haptics.
func (c *Config) IncreaseHapticsEnginePulseGain() float64 {
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

// DecreaseHapticsEnginePulseGain decreases the current engine pulse gain by the configured increment.
// If no profile is selected, it returns a gain level that silences engine haptics.
func (c *Config) DecreaseHapticsEnginePulseGain() float64 {
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

// GetHapticsEnginePulseScale returns the current engine pulse scale factor.
// If no profile is selected, it returns a scale factor of 1.0 (no scaling).
func (c *Config) GetHapticsEnginePulseScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 1.0
	}

	return c.viper.Haptics._engineProfile.PulseScale
}

// IncreaseHapticsEnginePulseScale increases the current engine pulse scale factor in increments of 0.01.
// If no profile is selected, it returns a scale factor of 1.0 (no scaling).
func (c *Config) IncreaseHapticsEnginePulseScale() float64 {
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

// DecreasehapticsEnginePulseScale decreases the current engine pulse scale factor in increments of 0.01.
// If no profile is selected, it returns a scale factor of 1.0 (no scaling).
func (c *Config) DecreasehapticsEnginePulseScale() float64 {
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

// IncreaseHapticsPulseMinHz increases the minimum pulse frequency in 1 Hz increments.
// This is the minimum frequency output for chassis bump haptics and is clamped to a maximum of 25Hz.
func (c *Config) IncreaseHapticsPulseMinHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMinFrequencyHz = min(25, c.viper.Haptics.PulseMinFrequencyHz+1)
	result := int(c.viper.Haptics.PulseMinFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// DecreaseHapticsPulseMinHz decreases the minimum pulse frequency in 1 Hz increments.
// This is the minimum frequency output for chassis bump haptics and is clamped to a minimum of 5Hz.
func (c *Config) DecreaseHapticsPulseMinHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMinFrequencyHz = max(5, c.viper.Haptics.PulseMinFrequencyHz-1)
	result := int(c.viper.Haptics.PulseMinFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// GetHapticsPulseMaxHz returns the configured maximum pulse frequency in Hz.
// This is the maximum frequency output for chassis bump haptics.
func (c *Config) GetHapticsPulseMaxHz() float64 {
	return c.snapshot.Load().PulseMaxFrequencyHz
}

// IncreaseHapticsPulseMaxHz increases the maximum pulse frequency in 1 Hz increments.
// This is the maximum frequency output for chassis bump haptics and is clamped to a maximum of 100Hz.
func (c *Config) IncreaseHapticsPulseMaxHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxFrequencyHz = min(100, c.viper.Haptics.PulseMaxFrequencyHz+1)
	result := int(c.viper.Haptics.PulseMaxFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// DecreasehapticsPulseMaxHz decreases the maximum pulse frequency in 1 Hz increments.
// This is the maximum frequency output for chassis bump haptics and is clamped to a minimum of 26Hz.
func (c *Config) DecreasehapticsPulseMaxHz() int {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxFrequencyHz = max(26, c.viper.Haptics.PulseMaxFrequencyHz-1)
	result := int(c.viper.Haptics.PulseMaxFrequencyHz)
	c.mu.Unlock()
	c.updatePulseWidthExtents()

	return result
}

// GetHapticePulseFrequencyHzRange returns the range between the configured minimum and maximum pulse frequencies in Hz.
// This is the frequency range output for chassis bump haptics.
func (c *Config) GetHapticePulseFrequencyHzRange() float64 {
	snap := c.snapshot.Load()

	return snap.PulseMaxFrequencyHz - snap.PulseMinFrequencyHz
}

// GetHapticsPulseWidthMin returns the minimum pulse width in samples based on the current max frequency.
func (c *Config) GetHapticsPulseWidthMin() float64 {
	return c.snapshot.Load().PulseWidthMin
}

// GetHapticsPulseWidthMax returns the maximum pulse width in samples based on the current min and max frequencies.
func (c *Config) GetHapticsPulseWidthMax() float64 {
	return c.snapshot.Load().PulseWidthMax
}

// GetHapticsPulseMaxAmplitude returns the maximum pulse amplitude for chassis bump haptics.
func (c *Config) GetHapticsPulseMaxAmplitude() float64 {
	return c.snapshot.Load().PulseMaxAmplitude
}

// SetHapticsPulseMaxAmplitude sets the maximum pulse amplitude for chassis bump haptics.
func (c *Config) SetHapticsPulseMaxAmplitude(value float64) {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxAmplitude = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetHapticsPulseMaxFrequencyHz returns the maximum pulse frequency in Hz for chassis bump haptics.
func (c *Config) GetHapticsPulseMaxFrequencyHz() float64 {
	return c.snapshot.Load().PulseMaxFrequencyHz
}

// SetHapticsPulseMaxFrequencyHz sets the maximum pulse frequency in Hz for chassis bump haptics.
func (c *Config) SetHapticsPulseMaxFrequencyHz(value float64) {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxFrequencyHz = value
	c.updatePulseWidthExtents()
	c.mu.Unlock()
}

// SetHapticsPulseMinFrequencyHz sets the minimum pulse frequency in Hz for chassis bump haptics.
func (c *Config) SetHapticsPulseMinFrequencyHz(value float64) {
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

// GetPitRadioMessageSendIntervalMs returns the interval in milliseconds between sending of pit radio messages.
func (c *Config) GetPitRadioMessageSendIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.MessageSendIntervalMs
}

// SetPitRadioMessageSendIntervalMs sets the interval in milliseconds between sending of pit radio messages.
func (c *Config) SetPitRadioMessageSendIntervalMs(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.MessageSendIntervalMs = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyRaceProgressEnabled returns whether race progress notifications are enabled.
func (c *Config) GetPitRadioNotifyRaceProgressEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.RaceProgressEnabled
}

// SetPitRadioNotifyRaceProgressEnabled sets whether race progress notifications are enabled.
func (c *Config) SetPitRadioNotifyRaceProgressEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceProgressEnabled = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyRaceProgressMinLaps returns the minimum number of laps before race progress notifications begin.
func (c *Config) GetPitRadioNotifyRaceProgressMinLaps() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.RaceProgressMinLaps
}

// SetPitRadioNotifyRaceProgressMinLaps sets the minimum number of laps before race progress notifications begin.
func (c *Config) SetPitRadioNotifyRaceProgressMinLaps(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceProgressMinLaps = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyRaceProgressIntervalPc returns the race progress notification interval percentage.
func (c *Config) GetPitRadioNotifyRaceProgressIntervalPc() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.RaceProgressIntervalPc
}

// SetPitRadioNotifyRaceProgressIntervalPc sets the race progress notification interval percentage.
func (c *Config) SetPitRadioNotifyRaceProgressIntervalPc(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceProgressIntervalPc = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyRaceLapsEnabled returns whether race lap notifications are enabled.
func (c *Config) GetPitRadioNotifyRaceLapsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.RaceLapsEnabled
}

// SetPitRadioNotifyRaceLapsEnabled sets whether race lap notifications are enabled.
func (c *Config) SetPitRadioNotifyRaceLapsEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceLapsEnabled = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyRaceLapsIntervalLaps returns the interval in laps for race lap notifications.
func (c *Config) GetPitRadioNotifyRaceLapsIntervalLaps() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.RaceLapsIntervalLaps
}

// SetPitRadioNotifyRaceLapsIntervalLaps sets the interval in laps for race lap notifications.
func (c *Config) SetPitRadioNotifyRaceLapsIntervalLaps(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceLapsIntervalLaps = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyRaceLapsCountdownLaps returns the number of laps for countdown notifications.
func (c *Config) GetPitRadioNotifyRaceLapsCountdownLaps() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.RaceLapsCountdownLaps
}

// SetPitRadioNotifyRaceLapsCountdownLaps sets the number of laps for countdown notifications.
func (c *Config) SetPitRadioNotifyRaceLapsCountdownLaps(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceLapsCountdownLaps = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyLapTimesEnabled returns whether lap time notifications are enabled.
func (c *Config) GetPitRadioNotifyLapTimesEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.LapTimesEnabled
}

// SetPitRadioNotifyLapTimesEnabled sets whether lap time notifications are enabled.
func (c *Config) SetPitRadioNotifyLapTimesEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.LapTimesEnabled = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyLapTimesMaxDeltaSeconds returns the maximum delta in seconds for lap time notifications.
func (c *Config) GetPitRadioNotifyLapTimesMaxDeltaSeconds() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds
}

// SetPitRadioNotifyLapTimesMaxDeltaSeconds sets the maximum delta in seconds for lap time notifications.
func (c *Config) SetPitRadioNotifyLapTimesMaxDeltaSeconds(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds = value

	c.registerUpdate(false)
}

// GetPitRadioNotifyCircuitMatchingEnabled returns whether circuit change notifications are enabled.
func (c *Config) GetPitRadioNotifyCircuitMatchingEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.CircuitMatchingEnabled
}

// SetPitRadioNotifyCircuitMatchingEnabled sets whether circuit change notifications are enabled.
func (c *Config) SetPitRadioNotifyCircuitMatchingEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.CircuitMatchingEnabled = value

	c.registerUpdate(false)
}

// GetPitRadioFuelMonitoringEnabled returns true if fuel monitoring is enabled.
func (c *Config) GetPitRadioFuelMonitoringEnabled() bool {
	return c.snapshot.Load().FuelMonitoringEnabled
}

// SetPitRadioFuelMonitoringEnabled sets whether fuel monitoring is enabled.
func (c *Config) SetPitRadioFuelMonitoringEnabled(value bool) {
	c.mu.Lock()
	c.viper.PitRadio.FuelMonitoring.Enabled = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetPitRadioFuelPreWarnNotifyLaps returns the number of laps remaining before a fuel pre-warning is triggered.
func (c *Config) GetPitRadioFuelPreWarnNotifyLaps() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps
}

// SetPitRadioFuelPreWarnNotifyLaps sets the number of laps remaining before a fuel pre-warning is triggered.
func (c *Config) SetPitRadioFuelPreWarnNotifyLaps(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps = value

	c.registerUpdate(false)
}

// GetPitRadioFuelStrategyNotifyLaps returns the number of laps remaining before a fuel strategy notification is triggered.
func (c *Config) GetPitRadioFuelStrategyNotifyLaps() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps
}

// SetPitRadioFuelStrategyNotifyLaps sets the number of laps remaining before a fuel strategy notification is triggered.
func (c *Config) SetPitRadioFuelStrategyNotifyLaps(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps = value

	c.registerUpdate(false)
}

// GetPitRadioFuelRangeSafetyMarginLaps returns the safety margin in laps to apply when calculating fuel range.
func (c *Config) GetPitRadioFuelRangeSafetyMarginLaps() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps
}

// SetPitRadioFuelRangeSafetyMarginLaps sets the safety margin in laps to apply when calculating fuel range.
func (c *Config) SetPitRadioFuelRangeSafetyMarginLaps(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps = value

	c.registerUpdate(false)
}

// GetPitRadioFuelRangeSafetyMarginMeters returns the safety margin in meters to apply when calculating fuel range.
func (c *Config) GetPitRadioFuelRangeSafetyMarginMeters() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMeters
}

// SetPitRadioFuelRangeSafetyMarginMeters sets the safety margin in meters to apply when calculating fuel range.
func (c *Config) SetPitRadioFuelRangeSafetyMarginMeters(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMeters = value

	c.registerUpdate(false)
}

// GetPitRadioTyreMonitoringEnabled returns whether tyre monitoring is enabled.
func (c *Config) GetPitRadioTyreMonitoringEnabled() bool {
	return c.snapshot.Load().TyreMonitoringEnabled
}

// SetPitRadioTyreMonitoringEnabled sets whether tyre monitoring is enabled.
func (c *Config) SetPitRadioTyreMonitoringEnabled(value bool) {
	c.mu.Lock()
	c.viper.PitRadio.TyreMonitoring.Enabled = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetPitRadioTyreTemperatureOptimalCelsius returns the optimal (center) tyre temperature in Celsius.
func (c *Config) GetPitRadioTyreTemperatureOptimalCelsius() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius
}

// SetPitRadioTyreTemperatureOptimalCelsius sets the optimal (center) tyre temperature in Celsius.
func (c *Config) SetPitRadioTyreTemperatureOptimalCelsius(value float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius = value

	c.registerUpdate(false)
}

// GetPitRadioTyreTemperatureOperatingWindow returns the total operating window width around optimal temperature in Celsius.
// The ideal temperature range is calculated as optimal ± (window/2).
func (c *Config) GetPitRadioTyreTemperatureOperatingWindow() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow
}

// SetPitRadioTyreTemperatureOperatingWindow sets the total operating window width around optimal temperature in Celsius.
func (c *Config) SetPitRadioTyreTemperatureOperatingWindow(value float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow = value

	c.registerUpdate(false)
}

// GetPitRadioTyreTemperatureMarginCelsius returns the margin beyond operating window for hot/cold thresholds in Celsius.
func (c *Config) GetPitRadioTyreTemperatureMarginCelsius() float32 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius
}

// SetPitRadioTyreTemperatureMarginCelsius sets the margin beyond operating window for hot/cold thresholds in Celsius.
func (c *Config) SetPitRadioTyreTemperatureMarginCelsius(value float32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius = value

	c.registerUpdate(false)
}

// ****************************************************************************
// Discord pit radio sub-section methods.
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
// Synthesizer methods.
// ****************************************************************************

// GetSynthesizer returns the synthesizer configuration.
func (c *Config) GetSynthesizer() *Synthesizer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer
}

// GetSynthInternalSampleRateHz returns the internal sample rate of the synthesizer in Hz.
// This is the sample rate at which the synthesizer processes audio.
// Lower values reduce CPU load and 8000 Hz should be more than sufficient for the haptic frequency range.
func (c *Config) GetSynthInternalSampleRateHz() int {
	return c.snapshot.Load().InternalSampleRateHz
}

// SetSynthInternalSampleRateHz sets the internal sample rate of the synthesizer in Hz.
func (c *Config) SetSynthInternalSampleRateHz(value int) {
	c.mu.Lock()
	c.viper.Synthesizer.InternalSampleRateHz = value
	c.updatePulseWidthExtents()
	c.rebuildSnapshot()
	c.registerUpdate(true)
	c.mu.Unlock()
}

// GetSynthOutputSampleRateHz returns the output sample rate of the synthesizer in Hz.
// This is the sample rate at which audio is output to the audio device or file.
// 32000 Hz is suitable for most common hardware but some may work at lower rates.
func (c *Config) GetSynthOutputSampleRateHz() int {
	return c.snapshot.Load().OutputSampleRateHz
}

// SetSynthOutputSampleRateHz sets the output sample rate of the synthesizer in Hz.
func (c *Config) SetSynthOutputSampleRateHz(value int) {
	c.mu.Lock()
	c.viper.Synthesizer.OutputSampleRateHz = value
	c.rebuildSnapshot()
	c.registerUpdate(true)
	c.mu.Unlock()
}

// GetSynthGainIncrement returns the gain increment value.
func (c *Config) GetSynthGainIncrement() float64 {
	return c.snapshot.Load().GainIncrement
}

// SetSynthGainIncrement sets the gain increment value.
func (c *Config) SetSynthGainIncrement(value float64) {
	c.mu.Lock()

	value = max(0.01, value)
	value = min(10, value)
	c.viper.Synthesizer.GainIncrement = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetSynthMasterGain returns the master gain of the synthesizer (i.e. the overall volume level).
// This is a global gain applied to all haptic feedback.
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) GetSynthMasterGain() float64 {
	return c.snapshot.Load().MasterGain
}

// SetSynthMasterGain sets the master gain of the synthesizer.
// This is a global gain applied to all haptic feedback.
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) SetSynthMasterGain(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.MasterGain = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetSynthMasterMute returns whether the master gain is muted.
func (c *Config) GetSynthMasterMute() bool {
	return c.snapshot.Load().MasterMute
}

// SetSynthMasterMute sets whether the master gain is muted.
func (c *Config) SetSynthMasterMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.MasterMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseSynthMasterGain increases the master gain by the configured gain increment.
func (c *Config) IncreaseSynthMasterGain() float64 {
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

// DecreaseSynthMasterGain decreases the master gain by the configured gain increment.
func (c *Config) DecreaseSynthMasterGain() float64 {
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

// GetSynthChassisGain returns the chassis gain of the synthesizer (i.e. the volume level for chassis bump haptics).
// 0.0 is maximum gain and -60.0 will mute chassis bump haptic output.
func (c *Config) GetSynthChassisGain() float64 {
	return c.snapshot.Load().ChassisGain
}

// GetSynthChassisMute returns whether the chassis gain is muted.
func (c *Config) GetSynthChassisMute() bool {
	return c.snapshot.Load().ChassisMute
}

// SetSynthChassisMute sets whether the chassis gain is muted.
func (c *Config) SetSynthChassisMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.ChassisMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetSynthChassisGain sets the chassis gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute chassis bump haptic output.
func (c *Config) SetSynthChassisGain(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.ChassisGain = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseSynthChassisGain increases the chassis gain by the configured gain increment.
func (c *Config) IncreaseSynthChassisGain() float64 {
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

// DecreaseSynthChassisGain decreases the chassis gain by the configured gain increment.
func (c *Config) DecreaseSynthChassisGain() float64 {
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

// SetSynthTransmissionGainMinRace sets the minimum transmission gain for race transmissions.
func (c *Config) SetSynthTransmissionGainMinRace(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinRace = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetSynthTransmissionGainMinStreet sets the minimum transmission gain for street transmissions.
func (c *Config) SetSynthTransmissionGainMinStreet(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinStreet = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetSynthTransmissionGain returns the transmission gain of the synthesizer (i.e. the volume level for transmission
// haptics).
// 0.0 is maximum gain and -60.0 will mute transmission haptic output.
func (c *Config) GetSynthTransmissionGain() float64 {
	return c.snapshot.Load().TransmissionGain
}

// GetSynthTransmissionMute returns whether the transmission gain is muted.
func (c *Config) GetSynthTransmissionMute() bool {
	return c.snapshot.Load().TransmissionMute
}

// SetSynthTransmissionMute sets whether the transmission gain is muted.
func (c *Config) SetSynthTransmissionMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetSynthTransmissionGain sets the transmission gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute transmission haptic output.
func (c *Config) SetSynthTransmissionGain(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGain = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseSynthTransmissionGain increases the transmission gain by the configured gain increment.
func (c *Config) IncreaseSynthTransmissionGain() float64 {
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

// DecreaseSynthTransmissionGain decreases the transmission gain by the configured gain increment.
func (c *Config) DecreaseSynthTransmissionGain() float64 {
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

// GetSynthTransmissionGainMinRace returns the minimum transmission gain for race vehicle types.
// This is the transnmission haptic feebdack volume applied when the vehicle is stationary.
func (c *Config) GetSynthTransmissionGainMinRace() float64 {
	return c.snapshot.Load().TransmissionGainMinRace
}

// GetSynthTransmissionGainMinStreet returns the minimum transmission gain for street vehicle types.
// This is the transmission haptic feedback volume applied when the vehicle is stationary.
func (c *Config) GetSynthTransmissionGainMinStreet() float64 {
	return c.snapshot.Load().TransmissionGainMinStreet
}

// GetSynthEngineGain returns the gain fir the currently selected engine (i.e. the volume level for engine haptics).
// 0.0 is maximum gain and -60.0 will mute engine haptic output.
func (c *Config) GetSynthEngineGain() float64 {
	return c.snapshot.Load().EngineGain
}

// GetSynthEngineMute returns whether the engine gain is muted.
func (c *Config) GetSynthEngineMute() bool {
	return c.snapshot.Load().EngineMute
}

// SetSynthEngineMute sets whether the engine gain is muted.
func (c *Config) SetSynthEngineMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.EngineMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetSynthEngineGain sets the engine gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute engine haptic output.
func (c *Config) SetSynthEngineGain(gain float64) {
	c.mu.Lock()
	c.viper.Synthesizer.EngineGain = max(MinimumGain, min(MaximumGain, gain))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseSynthEngineGain increases the gain of the currently selected engine by the configured gain increment.
func (c *Config) IncreaseSynthEngineGain() float64 {
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

// DecreaseSynthEngineGain decreases the gain of the currently selected engine by the configured gain increment.
func (c *Config) DecreaseSynthEngineGain() float64 {
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

// GetSynthOutputFile returns the synthesizer output file path.
func (c *Config) GetSynthOutputFile() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.OutputFile
}

// GetSynthEngineProfiles returns all engine profiles as a map.
func (c *Config) GetSynthEngineProfiles() map[string]appHaptics.EngineProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.EngineProfiles
}

// SetSynthEngineProfile updates or creates an engine profile.
func (c *Config) SetSynthEngineProfile(name string, profile appHaptics.EngineProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = strings.ToLower(name)
	c.viper.Haptics.EngineProfiles[name] = profile

	c.registerUpdate(false)
}

// GetSynthEqEnabled returns whether the equalizer is enabled.
func (c *Config) GetSynthEqEnabled() bool {
	return c.snapshot.Load().EqEnabled
}

// SetSynthEqEnabled sets whether the equalizer is enabled.
func (c *Config) SetSynthEqEnabled(enabled bool) {
	c.mu.Lock()
	c.viper.Synthesizer.EqEnabled = enabled
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// GetSynthEq returns the equalizer bands.
func (c *Config) GetSynthEq() []EQBand {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.EqBands
}

// SetSynthEq sets the equalizer bands and recomputes the curve.
func (c *Config) SetSynthEq(bands []EQBand) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(bands) == 8 {
		c.viper.Synthesizer.EqBands = bands
		c.computeEqCurve()
		c.registerUpdate(false)
	}
}

// GetSynthEqCurve returns the computed EQ curve for fast lookup.
// Returns the curve, minimum frequency, and resolution (Hz per bucket).
func (c *Config) GetSynthEqCurve() ([]float64, float64, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer._eqCurve,
		c.viper.Synthesizer._eqMinFreq,
		c.viper.Synthesizer._eqResolution
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
	const (
		minFreqHz    = 10.0
		maxFreqHz    = 70.0
		resolutionHz = 0.5
	)

	numBuckets := int((maxFreqHz-minFreqHz)/resolutionHz) + 1
	curve := make([]float64, numBuckets)

	// For each frequency bucket, compute the EQ response using bell filter
	for bucketNum := range numBuckets {
		freq := minFreqHz + float64(bucketNum)*resolutionHz

		// Start with unity gain (1.0 in linear, 0.0 in dB)
		amplitudeRatio := 1.0

		// Apply each band's bell filter by multiplication in linear space
		for _, band := range c.viper.Synthesizer.EqBands {
			// Calculate bell filter response at this frequency
			// Using per-band Q factor for bandwidth control
			if band.Gain != 0.0 {
				// Use band's qFactor value, default to 2.0 if not set
				qFactor := band.Q
				if qFactor <= 0 {
					qFactor = 2.0
				}

				freqRatio := freq / band.Frequency
				if freqRatio > 0 {
					// Bell filter magnitude response in dB
					// H(f) = G / sqrt(1 + Q^2 * (f/fc - fc/f)^2)
					// At center frequency (f = fc), delta = 0, denom = 1, so gain = G (exact)
					delta := freqRatio - 1.0/freqRatio
					denom := math.Sqrt(1.0 + qFactor*qFactor*delta*delta)

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
	c.viper.Synthesizer._eqMinFreq = minFreqHz
	c.viper.Synthesizer._eqMaxFreq = maxFreqHz
	c.viper.Synthesizer._eqResolution = resolutionHz
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
	newSnap := &Snapshot{
		MasterMute:                c.viper.Synthesizer.MasterMute,
		MasterGain:                c.viper.Synthesizer.MasterGain,
		ChassisMute:               c.viper.Synthesizer.ChassisMute,
		ChassisGain:               c.viper.Synthesizer.ChassisGain,
		TransmissionMute:          c.viper.Synthesizer.TransmissionMute,
		TransmissionGain:          c.viper.Synthesizer.TransmissionGain,
		TransmissionGainMinRace:   c.viper.Synthesizer.TransmissionGainMinRace,
		TransmissionGainMinStreet: c.viper.Synthesizer.TransmissionGainMinStreet,
		EngineMute:                c.viper.Synthesizer.EngineMute,
		EngineGain:                c.viper.Synthesizer.EngineGain,
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

		FuelMonitoringEnabled: c.viper.PitRadio.FuelMonitoring.Enabled,
		TyreMonitoringEnabled: c.viper.PitRadio.TyreMonitoring.Enabled,

		DisplayOrientation: c.viper.Hardware.DisplayOrientation,
	}
	c.snapshot.Store(newSnap)
}

// updatePulseWidthExtents recalculates the minimum and maximum pulse widths in samples.
// Assumes the caller does NOT hold the lock.
func (c *Config) updatePulseWidthExtents() {
	// Assumes caller holds c.mu.Lock()
	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMaxFrequencyHz)

	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMinFrequencyHz)

	c.rebuildSnapshot()
	c.registerUpdate(false)
}
