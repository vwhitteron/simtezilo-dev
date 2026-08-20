// Package config provides configuration management for the application.
package config

import (
	"bytes"
	"encoding/json"
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

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"github.com/vwhitteron/simtezilo-dev/app/haptics/profiles"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
)

const (
	fanModeManual = "manual"
	fanModeAuto   = "auto"
	fanModeAll    = "all"
)

// Bounds for the jerk pivot pair. The pivot is a plain jerk in m/s^3 and the gain
// a plain dB figure like every other gain in the config, with zero placing the
// pivot at full scale.
const (
	hapticsJerkPivotMin     = 1
	hapticsJerkPivotMax     = 20000
	hapticsJerkPivotGainMin = -12.0
	hapticsJerkPivotGainMax = 0.0
)

// Routing source keys identify the user-facing haptic sources that can be routed
// to output channels. The chassis row gates the internal per-channel chassis
// generators; engine and transmission are single mono buses.
const (
	RoutingSourceEngine       = "engine"
	RoutingSourceChassis      = "chassis"
	RoutingSourceTexture      = "texture"
	RoutingSourceTransmission = "transmission"
)

// routingSources is the canonical ordered list of routable sources.
var routingSources = []string{
	RoutingSourceEngine,
	RoutingSourceChassis,
	RoutingSourceTexture,
	RoutingSourceTransmission,
}

// defaultTransmissionJerkCurve is the shipped jerk response curve, in thousandths.
// It also backfills configs written before the key existed, which would otherwise
// read zero and collapse the mapping to full scale.
const defaultTransmissionJerkCurve = 750.0

type app struct {
	Language       string  `json:"language"`
	Accent         string  `json:"accent"`
	LogLevel       string  `json:"logLevel"`
	BaseDir        string  `json:"baseDir"`
	Update         *update `json:"update,omitempty"`
	VehicleDBFile  string  `json:"vehicleDBFile"` //nolint:tagliatelle // schema uses Go-style acronym
	EnabledWebUI   bool    `json:"enabledWebUI"`  //nolint:tagliatelle // schema uses Go-style acronym
	WebUIPort      int     `json:"webUIPort"`     //nolint:tagliatelle // schema uses Go-style acronym
	EnableDevTools bool    `json:"enableDevTools"`

	EnableExperimentalFeatures bool `json:"enableExperimentalFeatures"`

	// RealtimeScheduling asks the audio producer thread for a realtime
	// scheduling policy. It defaults to true. Set it to false to run the
	// producer at normal priority, which is the way to compare a tuned machine
	// against an untuned one without a rebuild.
	RealtimeScheduling bool `json:"realtimeScheduling"`
}

type discord struct {
	Token          string `json:"token"`
	GuildID        string `json:"guildID"`        //nolint:tagliatelle // schema uses Go-style acronym
	ChannelID      string `json:"channelID"`      //nolint:tagliatelle // schema uses Go-style acronym
	VoiceChannelID string `json:"voiceChannelID"` //nolint:tagliatelle // schema uses Go-style acronym
}

type fuelMonitoring struct {
	Enabled                 bool    `json:"enabled"`
	PreWarnNotifyLaps       float64 `json:"preWarnNotifyLaps"`
	StrategyNotifyLaps      float64 `json:"strategyNotifyLaps"`
	RangeSafetyMarginLaps   float64 `json:"rangeSafetyMarginLaps"`
	RangeSafetyMarginMetres float64 `json:"rangeSafetyMarginMetres"`
}

type haptics struct {
	Output                      HapticsOutput `json:"output"` // haptic feedback output stream
	EnableReplay                bool          `json:"enableReplay"`
	DynamicTransmissionFeedback bool          `json:"dynamicTransmissionFeedback"`
	// DynamicTransmissionJerkCurve is the driveline response curve, in thousandths,
	// applied to the normalised drive magnitude before it is clamped to the
	// vehicle's gain floor.
	DynamicTransmissionJerkCurve int `json:"dynamicTransmissionJerkCurve"`
	// DynamicTransmissionStepBlend is the depth to which this shift's driveline
	// step multiplies the learned per-vehicle character:
	// drive = character * (1 - blend + blend*event). At 0 a shift plays the
	// vehicle's character flat; at 1 a typical (gearShiftStepMax) shift plays that
	// character unchanged while smaller and larger steps scale it down and up in
	// proportion. Multiplying (rather than adding) the event keeps vehicles
	// ranked by their gearbox character instead of letting a soft gearbox borrow
	// loudness from a wide ratio jump it cannot actually deliver.
	DynamicTransmissionStepBlend float64 `json:"dynamicTransmissionStepBlend"`
	JerkCurve                    int     `json:"jerkCurve"`
	JerkPivot                    int     `json:"jerkPivot"`
	JerkPivotGain                float64 `json:"jerkPivotGain"`
	// JerkMax is deprecated: it was replaced by JerkPivot/JerkPivotGain. A
	// non-zero value is converted on load and then zeroed, so omitempty drops it
	// from the file on the next write and the migration runs at most once.
	JerkMax               int                               `json:"jerkMax,omitempty"`
	_jerkScale            float64                           `json:"-"`
	SnapCurve             int                               `json:"snapCurve"`
	SnapMax               int                               `json:"snapMax"`
	_snapScale            float64                           `json:"-"`
	PulseMaxAmplitude     float64                           `json:"pulseMaxAmplitude"`
	PulseMaxFrequencyHz   float64                           `json:"pulseMaxFrequencyHz"`
	PulseMinFrequencyHz   float64                           `json:"pulseMinFrequencyHz"`
	_pulseWidthMax        float64                           `json:"-"`
	_pulseWidthMin        float64                           `json:"-"`
	TextureMinFrequencyHz float64                           `json:"textureMinFrequencyHz"` // lower edge of the road-texture noise band (low-speed brightness)
	TextureMaxFrequencyHz float64                           `json:"textureMaxFrequencyHz"` // upper edge of the road-texture noise band (high-speed brightness)
	EngineProfiles        map[string]profiles.EngineProfile `json:"engineProfiles,omitempty"`
	_engineProfile        *profiles.EngineProfile           `json:"-"`
	_engineProfileName    string                            `json:"-"`
}

type hardware struct {
	Model              string `json:"model"`
	DisplayOrientation int    `json:"displayOrientation"`
}

type fan struct {
	Enabled          bool   `json:"enabled"`
	Mode             string `json:"mode"`
	DeviceAddress    string `json:"deviceAddress"` // MAC address of the paired fan device (selected in the Bluetooth panel)
	DeviceName       string `json:"deviceName"`    // Cached friendly name of the paired fan device, shown when it is offline/unlisted
	CommandTimeoutMs int    `json:"commandTimeoutMs"`
	MaxSpeedKPH      int    `json:"maxSpeedKph"`
}

type notifications struct {
	EnableRaceProgress      bool    `json:"enabledRaceProgress"`
	RaceProgressMinLaps     int     `json:"raceProgressMinLaps"`
	RaceProgressIntervalPc  int     `json:"raceProgressIntervalPc"`
	EnableRaceLaps          bool    `json:"enabledRaceLaps"`
	RaceLapsIntervalLaps    int     `json:"raceLapsIntervalLaps"`
	RaceLapsCountdownLaps   int     `json:"raceLapsCountdownLaps"`
	EnableLapTimes          bool    `json:"enabledLapTimes"`
	LapTimesMaxDeltaSeconds float64 `json:"lapTimesMaxDeltaSeconds"`
	EnableCircuitMatching   bool    `json:"enabledCircuitMatching"`
}

type pitRadio struct {
	Enabled               bool            `json:"enabled"`
	Output                string          `json:"output"` // log | discord | audio
	Audio                 PitRadioAudio   `json:"audio"`  // local audio device used when output is "audio"
	MessageSendIntervalMs int             `json:"messageSendIntervalMs"`
	Notifications         *notifications  `json:"notifications,omitempty"`
	Discord               *discord        `json:"discord,omitempty"`
	FuelMonitoring        *fuelMonitoring `json:"fuelMonitoring,omitempty"`
	TyreMonitoring        *tyreMonitoring `json:"tyreMonitoring,omitempty"`
}

// HapticsOutput configures the haptic feedback output stream.
type HapticsOutput struct {
	Device     string `json:"device"`     // backend device ID ("" selects the default)
	DeviceName string `json:"deviceName"` // human-readable device name; the stable, backend-agnostic selection key (Device is a tiebreaker)
	Channels   int    `json:"channels"`   // number of haptic output channels
	SampleRate int    `json:"sampleRate"` // output sample rate in Hz
	LatencyMs  int    `json:"latencyMs"`  // requested buffer latency in milliseconds
	CushionMs  int    `json:"cushionMs"`  // mixer buffer pre-fill cushion in milliseconds
}

// PitRadioAudio configures local playback of pit-radio audio on a dedicated
// device, used when the pit-radio output mode is "audio". It opens its own
// output sink, separate from the main audio device.
type PitRadioAudio struct {
	Device     string `json:"device"`     // backend device ID ("" selects the default)
	DeviceName string `json:"deviceName"` // human-readable device name; the stable, backend-agnostic selection key (Device is a tiebreaker)
	SampleRate int    `json:"sampleRate"` // output sample rate in Hz
	Volume     int    `json:"volume"`     // playback volume as a percentage (0-100); mapped onto a logarithmic (perceptual) dB taper, 0 dB at 100% down to a floor near 0%
}

// EQBand represents a parametric equalizer band with center frequency, gain, and Q factor.
type EQBand struct {
	Frequency float64 `json:"frequency"` // Center frequency in Hz
	Gain      float64 `json:"gain"`      // Gain in dB (-12 to +6)
	Q         float64 `json:"q"`         // Q factor (0.1 to 20, higher = narrower)
}

// Synthesizer represents an audio synthesizer used for haptic feedback.
type Synthesizer struct {
	InternalSampleRateHz      int       `json:"internalSampleRateHz"`
	OutputFile                string    `json:"outputFile,omitempty"`
	MasterMute                bool      `json:"masterMute"`
	MasterGain                float64   `json:"masterGain"`
	ChannelMute               []bool    `json:"channelMute"`
	ChannelGain               []float64 `json:"channelGain"`
	ChannelName               []string  `json:"channelName"`
	ChassisMute               bool      `json:"chassisMute"`
	ChassisGain               float64   `json:"chassisGain"`
	TextureMute               bool      `json:"textureMute"`
	TextureGain               float64   `json:"textureGain"`
	TransmissionMute          bool      `json:"transmissionMute"`
	TransmissionGain          float64   `json:"transmissionGain"`
	TransmissionGainMinRace   float64   `json:"transmissionGainMinRace"`
	TransmissionGainMinStreet float64   `json:"transmissionGainMinStreet"`
	EngineMute                bool      `json:"engineMute"`
	EngineGain                float64   `json:"engineGain"`
	GainIncrement             float64   `json:"gainIncrement"`
	EnableEq                  []bool    `json:"enableEq"`
	EnableDRX                 bool      `json:"enableDrx"`
	// Routing maps each source ("engine", "chassis", "transmission") to a
	// per-output-channel enable mask. Each row has length numOutputChannels.
	Routing       map[string][]bool `json:"routing,omitempty"`
	EqBands       [][]EQBand        `json:"eqBands,omitempty"`
	_eqCurve      [][]float64       `json:"-"` // Computed curve for fast lookup (per channel)
	_eqMinFreq    float64           `json:"-"` // Minimum frequency for curve
	_eqMaxFreq    float64           `json:"-"` // Maximum frequency for curve
	_eqResolution float64           `json:"-"` // Frequency resolution (Hz per bucket)
	_drxHeadroom  []float64         `json:"-"` // Deepest EQ attenuation in dB per channel (DRX headroom)
}

// Telemetry represents the telemetry data source configuration.
type Telemetry struct {
	Source    string `json:"source"`
	UpdateURL string `json:"updateURL"` //nolint:tagliatelle // schema uses Go-style acronym
}

// Status represents the status of the configuration.
type Status struct {
	LastUpdate      int64
	RestartRequired bool
}

type tyreMonitoring struct {
	Enabled                    bool    `json:"enabled"`
	TemperatureOptimalCelsius  float32 `json:"temperatureOptimalCelsius"`
	TemperatureOperatingWindow float32 `json:"temperatureOperatingWindow"`
	TemperatureMarginCelsius   float32 `json:"temperatureMarginCelsius"`
}

type update struct {
	BaseURL              string `json:"baseURL"` //nolint:tagliatelle // schema uses Go-style acronym
	Channel              string `json:"channel"`
	AutoCheck            bool   `json:"autoCheck"`
	AutoInstall          bool   `json:"autoInstall"`
	CheckIntervalMinutes int    `json:"checkIntervalMinutes"`
}

type viperConfig struct {
	Schema        string       `json:"$schema,omitempty"`
	SchemaVersion string       `json:"schemaVersion"`
	App           *app         `json:"app,omitempty"`
	Hardware      *hardware    `json:"hardware,omitempty"`
	Haptics       *haptics     `json:"haptics,omitempty"`
	PitRadio      *pitRadio    `json:"pitRadio,omitempty"`
	Synthesizer   *Synthesizer `json:"synthesizer,omitempty"`
	Telemetry     *Telemetry   `json:"telemetry,omitempty"`
	Fan           *fan         `json:"fan,omitempty"`
}

// Snapshot holds frequently-accessed configuration values for lock-free reads.
type Snapshot struct {
	// Synthesizer gain settings
	MasterMute                bool
	MasterGain                float64
	ChannelMute               []bool
	ChannelGain               []float64
	ChannelName               []string
	ChassisMute               bool
	ChassisGain               float64
	TextureMute               bool
	TextureGain               float64
	TransmissionMute          bool
	TransmissionGain          float64
	TransmissionGainMinRace   float64
	TransmissionGainMinStreet float64
	EngineMute                bool
	EngineGain                float64
	GainIncrement             float64
	InternalSampleRateHz      int

	// Haptics jerk settings (chassis amplitude). The response is
	//
	//	amplitude(jerk) = JerkScale * jerk^(JerkCurve/1000)
	//
	// anchored so that a jerk of JerkPivot m/s^3 lands JerkPivotGain dB below full
	// scale. JerkCurve is the shaping knob; the pivot pair is calibration,
	// fixing which event counts as the reference and how strong it should feel.
	// Changing the curve rotates the response about that pivot rather than about
	// the ceiling, so the reference event holds its level as the shape changes.
	JerkCurve     float64
	JerkPivot     int
	JerkPivotGain float64
	JerkScale     float64

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

	// Haptics road-texture settings (continuous suspension-roughness layer). The
	// on/off control is the synthesizer texture mute and loudness is the texture
	// channel gain; these shape the signal.
	TextureMinFrequencyHz float64
	TextureMaxFrequencyHz float64

	// DRX (Dynamic Range Extension) setting
	DRXEnabled bool

	// Dynamic transmission settings
	DynamicTransmissionFeedback  bool
	DynamicTransmissionJerkCurve int
	DynamicTransmissionStepBlend float64

	// EQ settings (per channel)
	EqEnabled []bool

	// Output routing matrix: source -> per-output-channel enable mask
	Routing map[string][]bool

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

	// Load through a private viper instance. The package-level viper is global
	// state, so concurrent calls to New race on it.
	vConf := viper.New()

	vConf.SetEnvPrefix("SIMTEZILO")
	vConf.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))
	vConf.AutomaticEnv()
	vConf.SetConfigType("json")

	if opts.ConfigFile != "" {
		opts.Logger.Debug().Str("filename", opts.ConfigFile).Msg("Loading config file")

		vConf.SetConfigFile(opts.ConfigFile)
	} else {
		opts.Logger.Debug().Msg("No config file specified, searching default locations")

		vConf.SetConfigName("simtezilo.conf")
		vConf.AddConfigPath("/boot/firmware/simtezilo/")
		vConf.AddConfigPath("/boot/simtezilo/")
		vConf.AddConfigPath("/opt/simtezilo/etc/")
		vConf.AddConfigPath("/opt/simtezilo/")
		vConf.AddConfigPath(".")
	}

	err := vConf.ReadInConfig()
	if err != nil {
		log.Error().
			Str("filename", vConf.ConfigFileUsed()).
			Err(err).
			Msg("read config file")
	} else {
		err = vConf.Unmarshal(config.viper)
		if err != nil {
			log.Error().Err(err).Msg("unmarshal config")
		}

		config.configFile = vConf.ConfigFileUsed()
		log.Debug().Str("source", config.configFile).Msg("config loaded")

		// Initialize lastSavedConfig with current state to prevent false restart indicators
		jsonData, err := json.Marshal(config.viper)
		if err == nil {
			config.lastSavedConfig = jsonData
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

			err = json.Unmarshal(data, newConfig)
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

// IsRestartRequired returns true if a restart is required for configuration changes to take effect.
func (c *Config) IsRestartRequired() bool {
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

	return c.viper.App.EnabledWebUI
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

// GetAppUpdateAutoCheck returns whether automatic update checking is enabled.
func (c *Config) GetAppUpdateAutoCheck() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.Update.AutoCheck
}

// SetAppUpdateAutoCheck sets whether automatic update checking is enabled.
func (c *Config) SetAppUpdateAutoCheck(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.Update.AutoCheck = enabled

	c.registerUpdate(false)
}

// GetAppUpdateAutoInstall returns whether updates should be automatically installed.
func (c *Config) GetAppUpdateAutoInstall() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.Update.AutoInstall
}

// SetAppUpdateAutoInstall sets whether updates should be automatically installed.
func (c *Config) SetAppUpdateAutoInstall(autoInstall bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.Update.AutoInstall = autoInstall

	c.registerUpdate(false)
}

// GetAppUpdateCheckIntervalMinutes returns the update check interval in minutes.
func (c *Config) GetAppUpdateCheckIntervalMinutes() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.Update.CheckIntervalMinutes
}

// SetAppUpdateCheckIntervalMinutes sets the update check interval in minutes.
func (c *Config) SetAppUpdateCheckIntervalMinutes(minutes int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.Update.CheckIntervalMinutes = minutes

	c.registerUpdate(false)
}

// GetAppUpdateBaseURL returns the URL of the update manifest.
func (c *Config) GetAppUpdateBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.Update.BaseURL
}

// GetAppUpdateChannel returns the update channel (e.g., "stable", "beta").
func (c *Config) GetAppUpdateChannel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.Update.Channel
}

// SetAppUpdateChannel sets the update channel (e.g., "stable", "beta", "dev").
func (c *Config) SetAppUpdateChannel(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.Update.Channel = channel

	c.registerUpdate(false)
}

// GetDevToolsEnabled returns true if developer tools are enabled.
func (c *Config) GetDevToolsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.EnableDevTools
}

// SetDevToolsEnabled sets whether developer tools are enabled.
func (c *Config) SetDevToolsEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.EnableDevTools = enabled

	c.registerUpdate(false)
}

// GetExperimentalFeaturesEnabled returns true if experimental features are enabled.
func (c *Config) GetExperimentalFeaturesEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.EnableExperimentalFeatures
}

// SetExperimentalFeaturesEnabled sets whether experimental features are enabled.
func (c *Config) SetExperimentalFeaturesEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.EnableExperimentalFeatures = enabled

	c.registerUpdate(false)
}

// GetAppRealtimeScheduling reports whether the audio producer thread requests a
// realtime scheduling policy. A false value also disables the CPU pin, because
// the pin is only useful under that policy. The request still needs the
// operating-system privilege; a refusal is a warning, never fatal.
func (c *Config) GetAppRealtimeScheduling() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.RealtimeScheduling
}

// SetAppRealtimeScheduling enables or disables the realtime request for the
// audio producer thread.
func (c *Config) SetAppRealtimeScheduling(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.RealtimeScheduling = value

	// The policy belongs to the producer thread, which is created once at audio
	// startup, so a change takes effect at the next restart.
	c.registerUpdate(true)
}

// ****************************************************************************
// Fan methods.
// ****************************************************************************

// FanEnabled returns true if the fan controller is enabled.
func (c *Config) FanEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil {
		return false
	}

	return c.viper.Fan.Enabled
}

// SetFanEnabled sets whether the fan controller is enabled.
func (c *Config) SetFanEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.Enabled = value

	// No restart required: the fan control loop re-reads FanEnabled() live —
	// the idle gate picks up an enable, and runFanControlDutyCycle tears the
	// connection down to duty-0 on a disable.
	c.registerUpdate(false)
}

// GetFanMode returns the fan operating mode.
// Allowed values are: off, open, all.
func (c *Config) GetFanMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil {
		return fanModeManual
	}

	mode := strings.ToLower(strings.TrimSpace(c.viper.Fan.Mode))

	switch mode {
	case fanModeManual, fanModeAuto, fanModeAll:
		return mode
	default:
		return fanModeManual
	}
}

// IsFanModeValid returns true when the configured fan mode is valid.
func (c *Config) IsFanModeValid() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil {
		return true
	}

	mode := strings.ToLower(strings.TrimSpace(c.viper.Fan.Mode))

	switch mode {
	case "", fanModeManual, fanModeAuto, fanModeAll:
		return true
	default:
		return false
	}
}

// GetFanConfiguredMode returns the configured fan mode without normalization.
func (c *Config) GetFanConfiguredMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil {
		return ""
	}

	return c.viper.Fan.Mode
}

// GetFanDeviceAddress returns the MAC address of the paired fan device. It is
// empty until a device is selected in the Bluetooth panel.
func (c *Config) GetFanDeviceAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil {
		return ""
	}

	return c.viper.Fan.DeviceAddress
}

// GetFanCommandTimeoutMs returns the BLE command timeout in milliseconds.
func (c *Config) GetFanCommandTimeoutMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil || c.viper.Fan.CommandTimeoutMs <= 0 {
		return 5000
	}

	return c.viper.Fan.CommandTimeoutMs
}

// GetFanMaxSpeedKPH returns the vehicle speed in km/h that maps to 100% fan duty.
func (c *Config) GetFanMaxSpeedKPH() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil || c.viper.Fan.MaxSpeedKPH <= 0 {
		return 250
	}

	return c.viper.Fan.MaxSpeedKPH
}

// SetFanMode sets the fan operating mode.
func (c *Config) SetFanMode(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.Mode = value

	c.registerUpdate(false)
}

// SetFanDeviceAddress sets the MAC address of the paired fan device.
func (c *Config) SetFanDeviceAddress(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.DeviceAddress = value

	c.registerUpdate(false)
}

// GetFanDeviceName returns the cached friendly name of the paired fan device.
// It is used by the web UI to label the device when it is offline and so absent
// from the live Bluetooth list.
func (c *Config) GetFanDeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Fan == nil {
		return ""
	}

	return c.viper.Fan.DeviceName
}

// SetFanDeviceName caches the friendly name of the paired fan device, refreshed
// by the web UI whenever the device is seen in a scan or while connected.
func (c *Config) SetFanDeviceName(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.DeviceName = value

	c.registerUpdate(false)
}

// SetFanCommandTimeoutMs sets the BLE command timeout in milliseconds.
func (c *Config) SetFanCommandTimeoutMs(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.CommandTimeoutMs = value

	c.registerUpdate(false)
}

// SetFanMaxSpeedKPH sets the vehicle speed in km/h that maps to 100% fan duty.
func (c *Config) SetFanMaxSpeedKPH(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.MaxSpeedKPH = value

	c.registerUpdate(false)
}

// CycleFanMode cycles the fan mode forward or backward.
// The order is: off -> open -> all -> off.
func (c *Config) CycleFanMode(forward bool) string {
	modes := []string{fanModeManual, fanModeAuto, fanModeAll}

	c.mu.Lock()
	defer c.mu.Unlock()

	current := strings.ToLower(strings.TrimSpace(c.viper.Fan.Mode))

	idx := 0

	for i, m := range modes {
		if m == current {
			idx = i

			break
		}
	}

	if forward {
		idx = (idx + 1) % len(modes)
	} else {
		idx = (idx + len(modes) - 1) % len(modes)
	}

	c.viper.Fan.Mode = modes[idx]
	c.registerUpdate(false)

	return modes[idx]
}

// IncreaseFanMaxSpeedKPH increases the max speed by 10 km/h, capped at 500.
func (c *Config) IncreaseFanMaxSpeedKPH() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.MaxSpeedKPH = min(500, c.viper.Fan.MaxSpeedKPH+10)

	c.registerUpdate(false)

	return c.viper.Fan.MaxSpeedKPH
}

// DecreaseFanMaxSpeedKPH decreases the max speed by 10 km/h, floored at 50.
func (c *Config) DecreaseFanMaxSpeedKPH() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.MaxSpeedKPH = max(50, c.viper.Fan.MaxSpeedKPH-10)

	c.registerUpdate(false)

	return c.viper.Fan.MaxSpeedKPH
}

// IncreaseFanCommandTimeoutMs increases the command timeout by 500 ms, capped at 10000.
func (c *Config) IncreaseFanCommandTimeoutMs() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.CommandTimeoutMs = min(10000, c.viper.Fan.CommandTimeoutMs+100)

	c.registerUpdate(false)

	return c.viper.Fan.CommandTimeoutMs
}

// DecreaseFanCommandTimeoutMs decreases the command timeout by 100 ms, floored at 100.
func (c *Config) DecreaseFanCommandTimeoutMs() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Fan.CommandTimeoutMs = max(100, c.viper.Fan.CommandTimeoutMs-100)

	c.registerUpdate(false)

	return c.viper.Fan.CommandTimeoutMs
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

	c.rebuildSnapshot()
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
	value = min(value, 995)
	value = max(value, 5)

	c.mu.Lock()
	c.viper.Haptics.JerkCurve = value
	c.mu.Unlock()
	c.updateJerkScale()
}

// IncreaseHapticsJerkCurve increases the jerk curve value in increments of 5.
func (c *Config) IncreaseHapticsJerkCurve() int {
	c.mu.Lock()
	c.viper.Haptics.JerkCurve = min(995, c.viper.Haptics.JerkCurve+5)
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

// GetHapticsJerkPivot returns the pivot jerk value, in m/s^3.
// This is the reference event: the jerk whose amplitude is held at
// GetHapticsJerkPivotGain regardless of how the jerk curve is shaped.
func (c *Config) GetHapticsJerkPivot() int {
	return c.snapshot.Load().JerkPivot
}

// SetHapticsJerkPivot sets the pivot jerk value, in m/s^3.
func (c *Config) SetHapticsJerkPivot(value int) {
	value = min(value, hapticsJerkPivotMax)
	value = max(value, hapticsJerkPivotMin)

	c.mu.Lock()
	c.viper.Haptics.JerkPivot = value
	c.mu.Unlock()
	c.updateJerkScale()
}

// IncreaseHapticsJerkPivot increases the pivot jerk value in increments of 1.
func (c *Config) IncreaseHapticsJerkPivot() int {
	c.mu.Lock()
	c.viper.Haptics.JerkPivot = min(hapticsJerkPivotMax, c.viper.Haptics.JerkPivot+1)
	result := c.viper.Haptics.JerkPivot
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// DecreaseHapticsJerkPivot decreases the pivot jerk value in increments of 1.
func (c *Config) DecreaseHapticsJerkPivot() int {
	c.mu.Lock()
	c.viper.Haptics.JerkPivot = max(hapticsJerkPivotMin, c.viper.Haptics.JerkPivot-1)
	result := c.viper.Haptics.JerkPivot
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// GetHapticsJerkPivotGain returns the amplitude at the pivot jerk, in dB below
// full scale. Zero puts the pivot at full scale, reproducing the behaviour of the
// jerkMax knob this pair replaced.
func (c *Config) GetHapticsJerkPivotGain() float64 {
	return c.snapshot.Load().JerkPivotGain
}

// SetHapticsJerkPivotGain sets the amplitude at the pivot jerk, in dB below full
// scale.
func (c *Config) SetHapticsJerkPivotGain(value float64) {
	value = min(value, hapticsJerkPivotGainMax)
	value = max(value, hapticsJerkPivotGainMin)

	c.mu.Lock()
	c.viper.Haptics.JerkPivotGain = value
	c.mu.Unlock()
	c.updateJerkScale()
}

// IncreaseHapticsJerkPivotGain increases the pivot gain by the configured gain
// increment, matching the other gain controls.
func (c *Config) IncreaseHapticsJerkPivotGain() float64 {
	c.mu.Lock()
	c.viper.Haptics.JerkPivotGain = min(
		hapticsJerkPivotGainMax,
		roundGainDB(c.viper.Haptics.JerkPivotGain+c.viper.Synthesizer.GainIncrement),
	)
	result := c.viper.Haptics.JerkPivotGain
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// DecreaseHapticsJerkPivotGain decreases the pivot gain by the configured gain
// increment, matching the other gain controls.
func (c *Config) DecreaseHapticsJerkPivotGain() float64 {
	c.mu.Lock()
	c.viper.Haptics.JerkPivotGain = max(
		hapticsJerkPivotGainMin,
		roundGainDB(c.viper.Haptics.JerkPivotGain-c.viper.Synthesizer.GainIncrement),
	)
	result := c.viper.Haptics.JerkPivotGain
	c.mu.Unlock()
	c.updateJerkScale()

	return result
}

// roundGainDB snaps a gain to a hundredth of a dB — the precision the UI displays
// — so stepping the value repeatedly does not accumulate binary floating point
// error into the stored figure.
func roundGainDB(value float64) float64 {
	return math.Round(value*100) / 100
}

// GetHapticsReplayEnabled returns true if replay mode is enabled.
func (c *Config) GetHapticsReplayEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.EnableReplay
}

// SetHapticsEnableReplay sets whether haptics are generated for replays.
func (c *Config) SetHapticsEnableReplay(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.EnableReplay = value

	// Applied live (read per telemetry packet in telemetryActive); no restart required.
	c.registerUpdate(false)
}

// GetHapticsSnapCurve returns the snap curve value.
func (c *Config) GetHapticsSnapCurve() float64 {
	return c.snapshot.Load().SnapCurve
}

// SetHapticsSnapCurve sets the snap curve value.
// Values closer to 0 produce a more linear response.
// Values closer to 1 produce a more exponential response.
func (c *Config) SetHapticsSnapCurve(value int) {
	value = min(value, 995)
	value = max(value, 5)

	c.mu.Lock()
	c.viper.Haptics.SnapCurve = value
	c.mu.Unlock()

	c.updateSnapScale()
}

// IncreaseHapticsSnapCurve increases the snap curve value in increments of 5.
func (c *Config) IncreaseHapticsSnapCurve() int {
	c.mu.Lock()

	c.viper.Haptics.SnapCurve = min(
		995,
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
	value = min(value, 200)
	value = max(value, 1)

	c.mu.Lock()
	c.viper.Haptics.SnapMax = value
	c.mu.Unlock()

	c.updateSnapScale()
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

// GetHapticsTransmissionJerkCurve returns the driveline response curve for the
// gear-shift magnitude. Falls back to the shipped default when unset, so configs
// written before the key existed do not silently run at an exponent of zero.
func (c *Config) GetHapticsTransmissionJerkCurve() float64 {
	curve := c.snapshot.Load().DynamicTransmissionJerkCurve
	if curve <= 0 {
		return defaultTransmissionJerkCurve
	}

	return float64(curve)
}

// SetHapticsTransmissionJerkCurve sets the response curve for the jerk source.
func (c *Config) SetHapticsTransmissionJerkCurve(value int) {
	c.mu.Lock()

	value = min(value, 995)
	value = max(value, 5)
	c.viper.Haptics.DynamicTransmissionJerkCurve = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseHapticsTransmissionJerkCurve increases the jerk curve value in increments of 5.
func (c *Config) IncreaseHapticsTransmissionJerkCurve() int {
	c.mu.Lock()
	c.viper.Haptics.DynamicTransmissionJerkCurve = min(995, c.viper.Haptics.DynamicTransmissionJerkCurve+5)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Haptics.DynamicTransmissionJerkCurve
	c.mu.Unlock()

	return result
}

// DecreaseHapticsTransmissionJerkCurve decreases the jerk curve value in increments of 5.
func (c *Config) DecreaseHapticsTransmissionJerkCurve() int {
	c.mu.Lock()
	c.viper.Haptics.DynamicTransmissionJerkCurve = max(5, c.viper.Haptics.DynamicTransmissionJerkCurve-5)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Haptics.DynamicTransmissionJerkCurve
	c.mu.Unlock()

	return result
}

// GetHapticsTransmissionStepBlend returns the depth to which this shift's
// driveline step multiplies the learned per-vehicle character: at 0 a shift
// plays the vehicle's character flat, at 1 a typical shift plays that
// character unchanged while smaller and larger steps scale it down and up.
func (c *Config) GetHapticsTransmissionStepBlend() float64 {
	return c.snapshot.Load().DynamicTransmissionStepBlend
}

// SetHapticsTransmissionStepBlend sets the character-to-step multiplier depth
// for the driveline transmission source.
func (c *Config) SetHapticsTransmissionStepBlend(value float64) {
	value = math.Min(1.0, value)
	value = math.Max(0.0, value)

	c.mu.Lock()
	c.viper.Haptics.DynamicTransmissionStepBlend = value
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseHapticsTransmissionStepBlend increases the step blend in increments of 0.05.
func (c *Config) IncreaseHapticsTransmissionStepBlend() float64 {
	c.mu.Lock()

	c.viper.Haptics.DynamicTransmissionStepBlend = min(
		1.0,
		c.viper.Haptics.DynamicTransmissionStepBlend+0.05,
	)

	c.rebuildSnapshot()
	c.registerUpdate(false)

	result := c.viper.Haptics.DynamicTransmissionStepBlend
	c.mu.Unlock()

	return result
}

// DecreaseHapticsTransmissionStepBlend decreases the step blend in increments of 0.05.
func (c *Config) DecreaseHapticsTransmissionStepBlend() float64 {
	c.mu.Lock()

	c.viper.Haptics.DynamicTransmissionStepBlend = max(
		0.0,
		c.viper.Haptics.DynamicTransmissionStepBlend-0.05,
	)

	c.rebuildSnapshot()
	c.registerUpdate(false)

	result := c.viper.Haptics.DynamicTransmissionStepBlend
	c.mu.Unlock()

	return result
}

// GetHapticsPulseMinHz returns the configured minimum pulse frequency in Hz.
// This is the minimum frequency output for chassis bump haptics.
func (c *Config) GetHapticsPulseMinHz() float64 {
	return c.snapshot.Load().PulseMinFrequencyHz
}

// GetHapticsEngineProfile returns the currently selected engine profile.
// If no profile is selected, it returns nil.
func (c *Config) GetHapticsEngineProfile(name string) *profiles.EngineProfile {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = strings.ToLower(name)
	if profile, ok := c.viper.Haptics.EngineProfiles[name]; ok {
		c.viper.Haptics._engineProfile = &profile
		c.viper.Haptics._engineProfileName = name
	} else {
		c.viper.Haptics._engineProfile = nil
		c.viper.Haptics._engineProfileName = ""
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

	c.syncEngineProfileToMap()
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

	c.syncEngineProfileToMap()
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

	c.syncEngineProfileToMap()
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

	c.syncEngineProfileToMap()
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

	c.syncEngineProfileToMap()
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

	c.syncEngineProfileToMap()
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

	c.syncEngineProfileToMap()
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

	c.syncEngineProfileToMap()
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

// DecreaseHapticsPulseMaxHz decreases the maximum pulse frequency in 1 Hz increments.
// This is the maximum frequency output for chassis bump haptics and is clamped to a minimum of 26Hz.
func (c *Config) DecreaseHapticsPulseMaxHz() int {
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

// GetHapticsTextureMinHz returns the lower edge of the road-texture noise band.
func (c *Config) GetHapticsTextureMinHz() float64 {
	return c.snapshot.Load().TextureMinFrequencyHz
}

// GetHapticsTextureMaxHz returns the upper edge of the road-texture noise band.
func (c *Config) GetHapticsTextureMaxHz() float64 {
	return c.snapshot.Load().TextureMaxFrequencyHz
}

// SetHapticsTextureMinFrequencyHz sets the texture tone frequency used at low speed,
// clamped to 5..400 Hz.
func (c *Config) SetHapticsTextureMinFrequencyHz(value float64) {
	c.mu.Lock()
	c.viper.Haptics.TextureMinFrequencyHz = max(5, min(400, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetHapticsTextureMaxFrequencyHz sets the texture tone frequency approached at high
// speed, clamped to 5..400 Hz.
func (c *Config) SetHapticsTextureMaxFrequencyHz(value float64) {
	c.mu.Lock()
	c.viper.Haptics.TextureMaxFrequencyHz = max(5, min(400, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
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

// IncreaseHapticsPulseMaxAmplitude increases the maximum pulse amplitude for chassis bump haptics in increments of 0.01.
func (c *Config) IncreaseHapticsPulseMaxAmplitude() float64 {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxAmplitude = min(1.0, c.viper.Haptics.PulseMaxAmplitude+0.01)
	result := c.viper.Haptics.PulseMaxAmplitude
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()

	return result
}

// DecreaseHapticsPulseMaxAmplitude decreases the maximum pulse amplitude for chassis bump haptics in increments of 0.01.
func (c *Config) DecreaseHapticsPulseMaxAmplitude() float64 {
	c.mu.Lock()
	c.viper.Haptics.PulseMaxAmplitude = max(0.0, c.viper.Haptics.PulseMaxAmplitude-0.01)
	result := c.viper.Haptics.PulseMaxAmplitude
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()

	return result
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
// DRX (Dynamic Range Extension) section methods.
// ****************************************************************************

// GetSynthDRXEnabled returns whether DRX (Dynamic Range Extension) is enabled.
// When enabled, high impact events shift pulse frequency into EQ-attenuated ranges
// and bypass equalisation to produce stronger haptic output.
func (c *Config) GetSynthDRXEnabled() bool {
	snap := c.snapshot.Load()

	return snap.DRXEnabled
}

// SetSynthDRXEnabled sets whether DRX (Dynamic Range Extension) is enabled.
func (c *Config) SetSynthDRXEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Synthesizer.EnableDRX = enabled
	c.rebuildSnapshot()
	c.registerUpdate(false)
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

func (c *Config) GetPitRadioOutput() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.PitRadio.Output == "" {
		c.viper.PitRadio.Output = "log"
	}

	return c.viper.PitRadio.Output
}

// SetPitRadioOutput sets the pit radio output device.
func (c *Config) SetPitRadioOutput(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch value {
	case "discord", "audio":
		c.viper.PitRadio.Output = value
	default:
		c.viper.PitRadio.Output = "log"
	}

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

	return c.viper.PitRadio.Notifications.EnableRaceProgress
}

// SetPitRadioNotifyRaceProgressEnabled sets whether race progress notifications are enabled.
func (c *Config) SetPitRadioNotifyRaceProgressEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.EnableRaceProgress = value

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

// IncreasePitRadioNotifyRaceProgressMinLaps increases the minimum laps by 1 (max 50).
func (c *Config) IncreasePitRadioNotifyRaceProgressMinLaps() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceProgressMinLaps = min(50, c.viper.PitRadio.Notifications.RaceProgressMinLaps+1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceProgressMinLaps
}

// DecreasePitRadioNotifyRaceProgressMinLaps decreases the minimum laps by 1 (min 1).
func (c *Config) DecreasePitRadioNotifyRaceProgressMinLaps() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceProgressMinLaps = max(1, c.viper.PitRadio.Notifications.RaceProgressMinLaps-1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceProgressMinLaps
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

	c.viper.PitRadio.Notifications.RaceProgressIntervalPc = min(50, max(5, value))

	c.registerUpdate(false)
}

// IncreasePitRadioNotifyRaceProgressIntervalPc increases the interval by 5% (max 50).
func (c *Config) IncreasePitRadioNotifyRaceProgressIntervalPc() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceProgressIntervalPc = min(50, c.viper.PitRadio.Notifications.RaceProgressIntervalPc+5)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceProgressIntervalPc
}

// DecreasePitRadioNotifyRaceProgressIntervalPc decreases the interval by 5% (min 5).
func (c *Config) DecreasePitRadioNotifyRaceProgressIntervalPc() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceProgressIntervalPc = max(5, c.viper.PitRadio.Notifications.RaceProgressIntervalPc-5)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceProgressIntervalPc
}

// GetPitRadioNotifyRaceLapsEnabled returns whether race lap notifications are enabled.
func (c *Config) GetPitRadioNotifyRaceLapsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.EnableRaceLaps
}

// SetPitRadioNotifyRaceLapsEnabled sets whether race lap notifications are enabled.
func (c *Config) SetPitRadioNotifyRaceLapsEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.EnableRaceLaps = value

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

	c.viper.PitRadio.Notifications.RaceLapsIntervalLaps = min(50, max(1, value))

	c.registerUpdate(false)
}

// IncreasePitRadioNotifyRaceLapsIntervalLaps increases the interval by 1 lap (max 50).
func (c *Config) IncreasePitRadioNotifyRaceLapsIntervalLaps() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceLapsIntervalLaps = min(50, c.viper.PitRadio.Notifications.RaceLapsIntervalLaps+1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceLapsIntervalLaps
}

// DecreasePitRadioNotifyRaceLapsIntervalLaps decreases the interval by 1 lap (min 1).
func (c *Config) DecreasePitRadioNotifyRaceLapsIntervalLaps() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceLapsIntervalLaps = max(1, c.viper.PitRadio.Notifications.RaceLapsIntervalLaps-1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceLapsIntervalLaps
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

	c.viper.PitRadio.Notifications.RaceLapsCountdownLaps = min(25, max(1, value))

	c.registerUpdate(false)
}

// IncreasePitRadioNotifyRaceLapsCountdownLaps increases the countdown laps by 1 (max 25).
func (c *Config) IncreasePitRadioNotifyRaceLapsCountdownLaps() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceLapsCountdownLaps = min(25, c.viper.PitRadio.Notifications.RaceLapsCountdownLaps+1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceLapsCountdownLaps
}

// DecreasePitRadioNotifyRaceLapsCountdownLaps decreases the countdown laps by 1 (min 1).
func (c *Config) DecreasePitRadioNotifyRaceLapsCountdownLaps() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.RaceLapsCountdownLaps = max(1, c.viper.PitRadio.Notifications.RaceLapsCountdownLaps-1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.RaceLapsCountdownLaps
}

// GetPitRadioNotifyLapTimesEnabled returns whether lap time notifications are enabled.
func (c *Config) GetPitRadioNotifyLapTimesEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.EnableLapTimes
}

// SetPitRadioNotifyLapTimesEnabled sets whether lap time notifications are enabled.
func (c *Config) SetPitRadioNotifyLapTimesEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.EnableLapTimes = value

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

	c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds = min(30.0, max(0.1, value))

	c.registerUpdate(false)
}

// IncreasePitRadioNotifyLapTimesMaxDeltaSeconds increases the maximum delta by 0.1 seconds (max 30.0).
func (c *Config) IncreasePitRadioNotifyLapTimesMaxDeltaSeconds() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds = min(30.0, c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds+0.1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds
}

// DecreasePitRadioNotifyLapTimesMaxDeltaSeconds decreases the maximum delta by 0.1 seconds (min 0.1).
func (c *Config) DecreasePitRadioNotifyLapTimesMaxDeltaSeconds() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds = max(0.1, c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds-0.1)
	c.registerUpdate(false)

	return c.viper.PitRadio.Notifications.LapTimesMaxDeltaSeconds
}

// GetPitRadioNotifyCircuitMatchingEnabled returns whether circuit change notifications are enabled.
func (c *Config) GetPitRadioNotifyCircuitMatchingEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Notifications.EnableCircuitMatching
}

// SetPitRadioNotifyCircuitMatchingEnabled sets whether circuit change notifications are enabled.
func (c *Config) SetPitRadioNotifyCircuitMatchingEnabled(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Notifications.EnableCircuitMatching = value

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

	c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps = min(10.0, max(0.0, value))

	c.registerUpdate(false)
}

// IncreasePitRadioFuelPreWarnNotifyLaps increases the pre-warn laps by 0.1 (max 10.0).
func (c *Config) IncreasePitRadioFuelPreWarnNotifyLaps() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps = min(10.0, c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps+0.1)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps
}

// DecreasePitRadioFuelPreWarnNotifyLaps decreases the pre-warn laps by 0.1 (min 0.0).
func (c *Config) DecreasePitRadioFuelPreWarnNotifyLaps() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps = max(0.0, c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps-0.1)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.PreWarnNotifyLaps
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

	c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps = min(20.0, max(0.0, value))

	c.registerUpdate(false)
}

// IncreasePitRadioFuelStrategyNotifyLaps increases the strategy notify laps by 0.1 (max 20.0).
func (c *Config) IncreasePitRadioFuelStrategyNotifyLaps() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps = min(20.0, c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps+0.1)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps
}

// DecreasePitRadioFuelStrategyNotifyLaps decreases the strategy notify laps by 0.1 (min 0.0).
func (c *Config) DecreasePitRadioFuelStrategyNotifyLaps() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps = max(0.0, c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps-0.1)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.StrategyNotifyLaps
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

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps = min(2.0, max(0.0, value))

	c.registerUpdate(false)
}

// IncreasePitRadioFuelRangeSafetyMarginLaps increases the safety margin by 0.05 laps (max 2.0).
func (c *Config) IncreasePitRadioFuelRangeSafetyMarginLaps() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps = min(2.0, c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps+0.05)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps
}

// DecreasePitRadioFuelRangeSafetyMarginLaps decreases the safety margin by 0.05 laps (min 0.0).
func (c *Config) DecreasePitRadioFuelRangeSafetyMarginLaps() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps = max(0.0, c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps-0.05)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginLaps
}

// GetPitRadioFuelRangeSafetyMarginMetres returns the safety margin in metres to apply when calculating fuel range.
func (c *Config) GetPitRadioFuelRangeSafetyMarginMetres() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres
}

// SetPitRadioFuelRangeSafetyMarginMetres sets the safety margin in metres to apply when calculating fuel range.
func (c *Config) SetPitRadioFuelRangeSafetyMarginMetres(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres = min(2000.0, max(0.0, value))

	c.registerUpdate(false)
}

// IncreasePitRadioFuelRangeSafetyMarginMetres increases the safety margin by 50 metres (max 2000).
func (c *Config) IncreasePitRadioFuelRangeSafetyMarginMetres() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres = min(2000.0, c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres+50)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres
}

// DecreasePitRadioFuelRangeSafetyMarginMetres decreases the safety margin by 50 metres (min 0).
func (c *Config) DecreasePitRadioFuelRangeSafetyMarginMetres() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres = max(0.0, c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres-50)
	c.registerUpdate(false)

	return c.viper.PitRadio.FuelMonitoring.RangeSafetyMarginMetres
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

	c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius = min(120.0, max(60.0, value))

	c.registerUpdate(false)
}

// IncreasePitRadioTyreTemperatureOptimalCelsius increases the optimal temperature by 1°C (max 120).
func (c *Config) IncreasePitRadioTyreTemperatureOptimalCelsius() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius = min(120.0, c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius+1)
	c.registerUpdate(false)

	return c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius
}

// DecreasePitRadioTyreTemperatureOptimalCelsius decreases the optimal temperature by 1°C (min 60).
func (c *Config) DecreasePitRadioTyreTemperatureOptimalCelsius() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius = max(60.0, c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius-1)
	c.registerUpdate(false)

	return c.viper.PitRadio.TyreMonitoring.TemperatureOptimalCelsius
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

	c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow = min(20.0, max(0.5, value))

	c.registerUpdate(false)
}

// IncreasePitRadioTyreTemperatureOperatingWindow increases the operating window by 0.5°C (max 20.0).
func (c *Config) IncreasePitRadioTyreTemperatureOperatingWindow() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow = min(20.0, c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow+0.5)
	c.registerUpdate(false)

	return c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow
}

// DecreasePitRadioTyreTemperatureOperatingWindow decreases the operating window by 0.5°C (min 0.5).
func (c *Config) DecreasePitRadioTyreTemperatureOperatingWindow() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow = max(0.5, c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow-0.5)
	c.registerUpdate(false)

	return c.viper.PitRadio.TyreMonitoring.TemperatureOperatingWindow
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

	c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius = min(10.0, max(0.5, value))

	c.registerUpdate(false)
}

// IncreasePitRadioTyreTemperatureMarginCelsius increases the temperature margin by 0.5°C (max 10.0).
func (c *Config) IncreasePitRadioTyreTemperatureMarginCelsius() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius = min(10.0, c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius+0.5)
	c.registerUpdate(false)

	return c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius
}

// DecreasePitRadioTyreTemperatureMarginCelsius decreases the temperature margin by 0.5°C (min 0.5).
func (c *Config) DecreasePitRadioTyreTemperatureMarginCelsius() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius = max(0.5, c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius-0.5)
	c.registerUpdate(false)

	return c.viper.PitRadio.TyreMonitoring.TemperatureMarginCelsius
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

// GetSynthChannelGain returns the gain for a specific channel of the synthesizer
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) GetSynthChannelGain(channel int) float64 {
	snap := c.snapshot.Load()
	if channel < 0 || channel >= len(snap.ChannelGain) {
		return 0.0
	}

	return snap.ChannelGain[channel]
}

// SetSynthChannelGain sets the gain for a specific channel of the synthesizer
// 0.0 is maximum gain and -60.0 will mute haptic output.
func (c *Config) SetSynthChannelGain(channel int, value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.ChannelGain) {
		return
	}

	c.viper.Synthesizer.ChannelGain[channel] = max(MinimumGain, min(MaximumGain, value))

	c.rebuildSnapshot()
	c.registerUpdate(false)
}

// GetSynthChannelMute returns whether a specific channel is muted.
func (c *Config) GetSynthChannelMute(channel int) bool {
	snap := c.snapshot.Load()
	if channel >= 0 && channel < len(snap.ChannelMute) {
		return snap.ChannelMute[channel]
	}

	return false
}

// SetSynthChannelMute sets whether a specific channel is muted.
func (c *Config) SetSynthChannelMute(channel int, mute bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.ChannelMute) {
		return
	}

	c.viper.Synthesizer.ChannelMute[channel] = mute

	c.rebuildSnapshot()
	c.registerUpdate(false)
}

// GetSynthChannelName returns the user-assigned display name for a specific
// channel. An empty string means the channel has no custom name and callers
// should fall back to a default label.
func (c *Config) GetSynthChannelName(channel int) string {
	snap := c.snapshot.Load()
	if channel >= 0 && channel < len(snap.ChannelName) {
		return snap.ChannelName[channel]
	}

	return ""
}

// SetSynthChannelName sets the user-assigned display name for a specific channel.
func (c *Config) SetSynthChannelName(channel int, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.ChannelName) {
		return
	}

	c.viper.Synthesizer.ChannelName[channel] = name

	c.rebuildSnapshot()
	c.registerUpdate(false)
}

// IncreaseSynthChannelGain increases the gain for a specific channel by the configured gain increment.
func (c *Config) IncreaseSynthChannelGain(channel int) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.ChannelGain) {
		return 0.0
	}

	currentGain := c.viper.Synthesizer.ChannelGain[channel]
	c.viper.Synthesizer.ChannelGain[channel] = min(
		MaximumGain,
		currentGain+c.viper.Synthesizer.GainIncrement,
	)
	result := c.viper.Synthesizer.ChannelGain[channel]

	c.rebuildSnapshot()
	c.registerUpdate(false)

	return result
}

// DecreaseSynthChannelGain decreases the gain for a specific channel by the configured gain increment.
func (c *Config) DecreaseSynthChannelGain(channel int) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.ChannelGain) {
		return 0.0
	}

	currentGain := c.viper.Synthesizer.ChannelGain[channel]
	c.viper.Synthesizer.ChannelGain[channel] = max(
		MinimumGain,
		currentGain-c.viper.Synthesizer.GainIncrement,
	)
	result := c.viper.Synthesizer.ChannelGain[channel]

	c.rebuildSnapshot()
	c.registerUpdate(false)

	return result
}

// normaliseRouting ensures the routing matrix has exactly the three known source
// rows, each of length numChannels. A source with no existing row is seeded with
// the default output routing: the primary stereo pair (channels 0 and 1, or just
// channel 0 on a mono device) enabled and any further channels disabled. Existing
// rows are preserved, with newly added channels defaulting to disabled. Unknown
// source rows are dropped. The caller must hold c.mu.
func (c *Config) normaliseRouting(numChannels int) {
	if c.viper.Synthesizer.Routing == nil {
		c.viper.Synthesizer.Routing = make(map[string][]bool, len(routingSources))
	}

	for _, source := range routingSources {
		row := c.viper.Synthesizer.Routing[source]
		if len(row) == numChannels {
			continue
		}

		newRow := make([]bool, numChannels)
		if len(row) == 0 {
			// Unconfigured source: enable the primary stereo pair (channel 0
			// on a mono device) and leave any additional channels disabled.
			for i := 0; i < numChannels && i < 2; i++ {
				newRow[i] = true
			}
		} else {
			// Preserve existing assignments; newly added channels default off.
			for i := 0; i < numChannels && i < len(row); i++ {
				newRow[i] = row[i]
			}
		}

		c.viper.Synthesizer.Routing[source] = newRow
	}

	// Drop any rows that are not recognised sources.
	for source := range c.viper.Synthesizer.Routing {
		if source != RoutingSourceEngine &&
			source != RoutingSourceChassis &&
			source != RoutingSourceTexture &&
			source != RoutingSourceTransmission {
			delete(c.viper.Synthesizer.Routing, source)
		}
	}
}

// GetSynthRouting returns a deep copy of the output routing matrix.
func (c *Config) GetSynthRouting() map[string][]bool {
	snap := c.snapshot.Load()

	out := make(map[string][]bool, len(snap.Routing))
	for source, row := range snap.Routing {
		dup := make([]bool, len(row))
		copy(dup, row)
		out[source] = dup
	}

	return out
}

// GetSynthRouteEnabled reports whether the given source is routed to the given
// output channel. Unknown sources or out-of-range channels return false.
func (c *Config) GetSynthRouteEnabled(source string, channel int) bool {
	snap := c.snapshot.Load()

	row, ok := snap.Routing[source]
	if !ok || channel < 0 || channel >= len(row) {
		return false
	}

	return row[channel]
}

// SetSynthRoute enables or disables routing of a source to an output channel.
func (c *Config) SetSynthRoute(source string, channel int, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	row, ok := c.viper.Synthesizer.Routing[source]
	if !ok || channel < 0 || channel >= len(row) {
		return
	}

	row[channel] = enabled

	c.rebuildSnapshot()
	c.registerUpdate(false)
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

// GetSynthTextureGain returns the texture gain of the synthesizer (i.e. the volume
// level for the continuous road-texture layer).
// 0.0 is maximum gain and -60.0 will mute texture output.
func (c *Config) GetSynthTextureGain() float64 {
	return c.snapshot.Load().TextureGain
}

// GetSynthTextureMute returns whether the texture gain is muted.
func (c *Config) GetSynthTextureMute() bool {
	return c.snapshot.Load().TextureMute
}

// SetSynthTextureMute sets whether the texture gain is muted.
func (c *Config) SetSynthTextureMute(mute bool) {
	c.mu.Lock()
	c.viper.Synthesizer.TextureMute = mute
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// SetSynthTextureGain sets the texture gain of the synthesizer.
// 0.0 is maximum gain and -60.0 will mute texture output.
func (c *Config) SetSynthTextureGain(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TextureGain = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseSynthTextureGain increases the texture gain by the configured gain increment.
func (c *Config) IncreaseSynthTextureGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TextureGain = min(
		MaximumGain,
		c.viper.Synthesizer.TextureGain+c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TextureGain
	c.mu.Unlock()

	return result
}

// DecreaseSynthTextureGain decreases the texture gain by the configured gain increment.
func (c *Config) DecreaseSynthTextureGain() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TextureGain = max(
		MinimumGain,
		c.viper.Synthesizer.TextureGain-c.viper.Synthesizer.GainIncrement,
	)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TextureGain
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

// IncreaseSynthTransmissionGainMinRace increases the minimum race transmission gain by 0.25 (max 0).
func (c *Config) IncreaseSynthTransmissionGainMinRace() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinRace = min(MaximumGain, c.viper.Synthesizer.TransmissionGainMinRace+0.25)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TransmissionGainMinRace
	c.mu.Unlock()

	return result
}

// DecreaseSynthTransmissionGainMinRace decreases the minimum race transmission gain by 0.25 (min -60).
func (c *Config) DecreaseSynthTransmissionGainMinRace() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinRace = max(MinimumGain, c.viper.Synthesizer.TransmissionGainMinRace-0.25)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TransmissionGainMinRace
	c.mu.Unlock()

	return result
}

// SetSynthTransmissionGainMinStreet sets the minimum transmission gain for street transmissions.
func (c *Config) SetSynthTransmissionGainMinStreet(value float64) {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinStreet = max(MinimumGain, min(MaximumGain, value))
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// IncreaseSynthTransmissionGainMinStreet increases the minimum street transmission gain by 0.25 (max 0).
func (c *Config) IncreaseSynthTransmissionGainMinStreet() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinStreet = min(MaximumGain, c.viper.Synthesizer.TransmissionGainMinStreet+0.25)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TransmissionGainMinStreet
	c.mu.Unlock()

	return result
}

// DecreaseSynthTransmissionGainMinStreet decreases the minimum street transmission gain by 0.25 (min -60).
func (c *Config) DecreaseSynthTransmissionGainMinStreet() float64 {
	c.mu.Lock()
	c.viper.Synthesizer.TransmissionGainMinStreet = max(MinimumGain, c.viper.Synthesizer.TransmissionGainMinStreet-0.25)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	result := c.viper.Synthesizer.TransmissionGainMinStreet
	c.mu.Unlock()

	return result
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
func (c *Config) GetSynthEngineProfiles() map[string]profiles.EngineProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.EngineProfiles
}

// SetSynthEngineProfile updates or creates an engine profile.
func (c *Config) SetSynthEngineProfile(name string, profile profiles.EngineProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()

	name = strings.ToLower(name)
	c.viper.Haptics.EngineProfiles[name] = profile

	c.registerUpdate(false)
}

// GetSynthChannelEqEnabled returns whether the equalizer is enabled for a specific channel.
func (c *Config) GetSynthChannelEqEnabled(channel int) bool {
	snap := c.snapshot.Load()
	if channel >= 0 && channel < len(snap.EqEnabled) {
		return snap.EqEnabled[channel]
	}

	return false
}

// SetSynthChannelEqEnabled sets whether the equalizer is enabled for a specific channel.
func (c *Config) SetSynthChannelEqEnabled(channel int, enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.EnableEq) {
		return
	}

	c.viper.Synthesizer.EnableEq[channel] = enabled
	c.rebuildSnapshot()
	c.registerUpdate(false)
}

// GetSynthChannelEq returns the equalizer bands for a specific channel.
func (c *Config) GetSynthChannelEq(channel int) []EQBand {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.EqBands) {
		return nil
	}

	return c.viper.Synthesizer.EqBands[channel]
}

// SetSynthChannelEq sets the equalizer bands for a specific channel and recomputes the curve.
func (c *Config) SetSynthChannelEq(channel int, bands []EQBand) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer.EqBands) {
		log.Warn().Int("channel", channel).Int("eqBandsLen", len(c.viper.Synthesizer.EqBands)).Msg("SetSynthChannelEq: channel out of range")

		return
	}

	if len(bands) == 8 {
		c.viper.Synthesizer.EqBands[channel] = bands
		c.computeEqCurve(channel)
		c.registerUpdate(false)
	}
}

// GetSynthChannelEqCurve returns the computed EQ curve for fast lookup for a specific channel.
// Returns the curve, minimum frequency, and resolution (Hz per bucket).
func (c *Config) GetSynthChannelEqCurve(channel int) ([]float64, float64, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer._eqCurve) {
		log.Warn().Int("channel", channel).Int("eqCurveLen", len(c.viper.Synthesizer._eqCurve)).Msg("GetSynthChannelEqCurve: channel out of range")

		return nil, 0, 0
	}

	return c.viper.Synthesizer._eqCurve[channel],
		c.viper.Synthesizer._eqMinFreq,
		c.viper.Synthesizer._eqResolution
}

// GetSynthChannelDRXHeadroom returns the deepest EQ attenuation in dB for the given channel.
// A value of 0.0 means no attenuation (no DRX headroom); negative values indicate available headroom.
func (c *Config) GetSynthChannelDRXHeadroom(channel int) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if channel < 0 || channel >= len(c.viper.Synthesizer._drxHeadroom) {
		return 0.0
	}

	return c.viper.Synthesizer._drxHeadroom[channel]
}

// GetSynthChannelsEqEnabled returns the EQ enabled state for all channels.
func (c *Config) GetSynthChannelsEqEnabled() []bool {
	snap := c.snapshot.Load()
	result := make([]bool, len(snap.EqEnabled))
	copy(result, snap.EqEnabled)

	return result
}

// GetSynthChannelsEq returns the EQ bands for all channels.
func (c *Config) GetSynthChannelsEq() [][]EQBand {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([][]EQBand, len(c.viper.Synthesizer.EqBands))
	for ch, bands := range c.viper.Synthesizer.EqBands {
		result[ch] = make([]EQBand, len(bands))
		copy(result[ch], bands)
	}

	return result
}

// GetSynthChannelsEqCurve returns the computed EQ curves for all channels.
// Returns the curves, minimum frequency, and resolution (Hz per bucket).
func (c *Config) GetSynthChannelsEqCurve() ([][]float64, float64, float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([][]float64, len(c.viper.Synthesizer._eqCurve))
	for ch, curve := range c.viper.Synthesizer._eqCurve {
		result[ch] = make([]float64, len(curve))
		copy(result[ch], curve)
	}

	return result, c.viper.Synthesizer._eqMinFreq, c.viper.Synthesizer._eqResolution
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

// GetTelemetryUpdateURL returns the configured telemetry update URL.
func (c *Config) GetTelemetryUpdateURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Telemetry.UpdateURL
}

// SetTelemetryUpdateURL sets the telemetry update URL.
func (c *Config) SetTelemetryUpdateURL(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Telemetry.UpdateURL = value

	c.registerUpdate(true)
}

// ****************************************************************************
// Audio device routing accessors.
//
// Audio settings route the haptic and pit-radio streams to specific devices.
// Device/channel/sample-rate/latency changes are applied live (the haptic
// stream is rebuilt and pit-radio resolves per message) and do not require a
// restart. The cushion is a restart-required value.
// ****************************************************************************

// GetAudioHapticsDevice returns the haptics output device ID ("" = default).
func (c *Config) GetAudioHapticsDevice() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.Output.Device
}

// SetAudioHapticsDevice sets the haptics output device ID.
func (c *Config) SetAudioHapticsDevice(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.Output.Device = value

	// Applied live (the haptic stream is rebuilt on change); no restart required.
	c.registerUpdate(false)
}

// GetAudioHapticsDeviceName returns the haptics output device name (the stable,
// backend-agnostic selection key).
func (c *Config) GetAudioHapticsDeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.Output.DeviceName
}

// SetAudioHapticsDeviceName sets the haptics output device name.
func (c *Config) SetAudioHapticsDeviceName(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.Output.DeviceName = value

	// Applied live (used when resolving the device on the next stream rebuild).
	c.registerUpdate(false)
}

// GetAudioHapticsChannels returns the number of haptic output channels.
func (c *Config) GetAudioHapticsChannels() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics.Output.Channels < 1 {
		return 2
	}

	return c.viper.Haptics.Output.Channels
}

// SetAudioHapticsChannels sets the number of haptic output channels.
func (c *Config) SetAudioHapticsChannels(value int) {
	c.mu.Lock()
	c.viper.Haptics.Output.Channels = value
	c.mu.Unlock()

	// Resize the per-channel synth arrays (gain, mute, EQ bands/enable, routing)
	// to the new channel count so channels added live are immediately addressable
	// with sane defaults, rather than reading max gain / stale EQ until the next
	// config reload. finalise manages its own locking (see the load path).
	c.finalise()
	c.rebuildSnapshot()

	// Applied live (the haptic stream is rebuilt on change); no restart required.
	c.mu.Lock()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// defaultHapticsOutputSampleRateHz is the last-resort output rate used when the
// haptics rate is unset/invalid and no output device can be enumerated for its
// native rate. 48 kHz is a near-universal default-device rate; the polyphase
// resampler bridges the synth internal rate to it. Callers that hold an audio
// backend (see startAudioOutput / captureThroughSink) prefer the actual output
// device's native rate over this value.
const defaultHapticsOutputSampleRateHz = 48000

// GetAudioHapticsSampleRate returns the haptics output sample rate in Hz. This is
// a pure config accessor: it returns the configured rate, or a safe default when
// that value is unset/invalid. Device-native rate resolution lives in the audio
// init paths that hold a backend handle, not here.
func (c *Config) GetAudioHapticsSampleRate() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics.Output.SampleRate < 8000 {
		return defaultHapticsOutputSampleRateHz
	}

	return c.viper.Haptics.Output.SampleRate
}

// SetAudioHapticsSampleRate sets the haptics output sample rate in Hz.
func (c *Config) SetAudioHapticsSampleRate(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.Output.SampleRate = value

	// Applied live (the haptic stream is rebuilt on change); no restart required.
	c.registerUpdate(false)
}

// GetAudioHapticsLatencyMs returns the requested haptics buffer latency in ms.
func (c *Config) GetAudioHapticsLatencyMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.Output.LatencyMs
}

// SetAudioHapticsLatencyMs sets the requested haptics buffer latency in ms.
func (c *Config) SetAudioHapticsLatencyMs(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.Output.LatencyMs = value

	// Applied live (the haptic stream is rebuilt on change); no restart required.
	c.registerUpdate(false)
}

// defaultHapticsCushionMs is the mixer buffer pre-fill cushion used when no
// value (or a non-positive one) is configured.
const defaultHapticsCushionMs = 24

// GetAudioHapticsCushionMs returns the mixer buffer pre-fill cushion in ms. This
// is how much audio each haptic channel buffer holds in reserve; it must comfortably
// exceed the per-read pull size to avoid underruns (and the resulting clicks) when
// a telemetry frame is briefly late.
func (c *Config) GetAudioHapticsCushionMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics.Output.CushionMs <= 0 {
		return defaultHapticsCushionMs
	}

	return c.viper.Haptics.Output.CushionMs
}

// SetAudioHapticsCushionMs sets the mixer buffer pre-fill cushion in ms.
func (c *Config) SetAudioHapticsCushionMs(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.Output.CushionMs = value

	c.registerUpdate(true)
}

// GetAudioPitRadioDevice returns the pit-radio output device ID ("" = default).
func (c *Config) GetAudioPitRadioDevice() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Audio.Device
}

// SetAudioPitRadioDevice sets the pit-radio output device ID.
func (c *Config) SetAudioPitRadioDevice(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Audio.Device = value

	// Applied live (resolved per message); no restart required.
	c.registerUpdate(false)
}

// GetAudioPitRadioDeviceName returns the pit-radio output device name (the
// stable, backend-agnostic selection key).
func (c *Config) GetAudioPitRadioDeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.PitRadio.Audio.DeviceName
}

// SetAudioPitRadioDeviceName sets the pit-radio output device name.
func (c *Config) SetAudioPitRadioDeviceName(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Audio.DeviceName = value

	// Applied live (resolved per message); no restart required.
	c.registerUpdate(false)
}

// GetAudioPitRadioSampleRate returns the pit-radio output sample rate in Hz.
func (c *Config) GetAudioPitRadioSampleRate() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.PitRadio.Audio.SampleRate < 1 {
		return 48000
	}

	return c.viper.PitRadio.Audio.SampleRate
}

// SetAudioPitRadioSampleRate sets the pit-radio output sample rate in Hz.
func (c *Config) SetAudioPitRadioSampleRate(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Audio.SampleRate = value

	// Applied live (resolved per message); no restart required.
	c.registerUpdate(false)
}

// GetAudioPitRadioVolume returns the pit-radio playback volume as a percentage
// (0-100). The value is clamped so a hand-edited config can never drive the
// linear gain outside the sensible range.
func (c *Config) GetAudioPitRadioVolume() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return min(100, max(0, c.viper.PitRadio.Audio.Volume))
}

// SetAudioPitRadioVolume sets the pit-radio playback volume percentage (0-100).
func (c *Config) SetAudioPitRadioVolume(value int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Audio.Volume = min(100, max(0, value))

	// Applied live (read per message); no restart required.
	c.registerUpdate(false)
}

// IncreaseAudioPitRadioVolume raises the pit-radio volume by 5% (max 100).
func (c *Config) IncreaseAudioPitRadioVolume() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Audio.Volume = min(100, max(0, c.viper.PitRadio.Audio.Volume)+5)
	c.registerUpdate(false)

	return c.viper.PitRadio.Audio.Volume
}

// DecreaseAudioPitRadioVolume lowers the pit-radio volume by 5% (min 0).
func (c *Config) DecreaseAudioPitRadioVolume() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.PitRadio.Audio.Volume = max(0, min(100, c.viper.PitRadio.Audio.Volume)-5)
	c.registerUpdate(false)

	return c.viper.PitRadio.Audio.Volume
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

	// Marshal the configuration to JSON
	jsonData, err := json.MarshalIndent(c.viper, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal configuration to JSON: %w", err)
	}

	// Skip write if config hasn't changed (reduces SD card wear)
	if bytes.Equal(jsonData, c.lastSavedConfig) {
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

	_, err = file.Write(jsonData)
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
	c.lastSavedConfig = jsonData

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

// defaultChannelGain seeds newly added output channels (dB). Matches the
// per-channel default in the shipped default config.
const defaultChannelGain = -30.0

// resizeBoolChannels returns a slice of length n, preserving existing values and
// filling any newly added channels with fill.
func resizeBoolChannels(s []bool, n int, fill bool) []bool {
	resized := make([]bool, n)
	for i := range n {
		if i < len(s) {
			resized[i] = s[i]
		} else {
			resized[i] = fill
		}
	}

	return resized
}

// resizeGainChannels returns a slice of length n, preserving existing values and
// seeding any newly added channels with fill.
func resizeGainChannels(s []float64, n int, fill float64) []float64 {
	resized := make([]float64, n)
	for i := range n {
		if i < len(s) {
			resized[i] = s[i]
		} else {
			resized[i] = fill
		}
	}

	return resized
}

// resizeStringChannels returns a slice of length n, preserving existing values
// and filling any newly added channels with fill.
func resizeStringChannels(s []string, n int, fill string) []string {
	resized := make([]string, n)
	for i := range n {
		if i < len(s) {
			resized[i] = s[i]
		} else {
			resized[i] = fill
		}
	}

	return resized
}

// finalise performs validation of the config and updates any derived configuration values.
func (c *Config) finalise() {
	c.mu.Lock()

	// Fold any deprecated jerkMax value into the pivot before the derived scale
	// is computed from it below.
	c.migrateJerkMax()

	// All per-channel synth arrays are sized to the configured output channel
	// count so that any channel index the pipeline addresses is valid.
	numChannels := c.viper.Haptics.Output.Channels
	if numChannels < 1 {
		numChannels = 2
	}

	defaultBands := []EQBand{
		{Frequency: 12, Gain: 0.0, Q: 2.0},
		{Frequency: 16, Gain: 0.0, Q: 2.0},
		{Frequency: 20, Gain: 0.0, Q: 2.0},
		{Frequency: 25, Gain: 0.0, Q: 2.0},
		{Frequency: 30, Gain: 0.0, Q: 2.0},
		{Frequency: 38, Gain: 0.0, Q: 2.0},
		{Frequency: 48, Gain: 0.0, Q: 2.0},
		{Frequency: 58, Gain: 0.0, Q: 2.0},
	}

	// Size EQ bands to the channel count, preserving existing per-channel bands
	// and seeding any newly added channels with the default curve.
	if len(c.viper.Synthesizer.EqBands) != numChannels {
		log.Debug().Int("length", len(c.viper.Synthesizer.EqBands)).Int("channels", numChannels).Msg("resizing synthesizer EQ bands to channel count")

		resized := make([][]EQBand, numChannels)
		for ch := range numChannels {
			if ch < len(c.viper.Synthesizer.EqBands) && c.viper.Synthesizer.EqBands[ch] != nil {
				resized[ch] = c.viper.Synthesizer.EqBands[ch]
			} else {
				resized[ch] = make([]EQBand, len(defaultBands))
				copy(resized[ch], defaultBands)
			}
		}

		c.viper.Synthesizer.EqBands = resized
	}

	// Validate each channel has 8 bands
	for ch := range c.viper.Synthesizer.EqBands {
		if len(c.viper.Synthesizer.EqBands[ch]) != 8 {
			log.Warn().Int("channel", ch).Int("length", len(c.viper.Synthesizer.EqBands[ch])).Msg("invalid EQ bands length for channel, initializing defaults")

			c.viper.Synthesizer.EqBands[ch] = make([]EQBand, len(defaultBands))
			copy(c.viper.Synthesizer.EqBands[ch], defaultBands)
		}
	}

	// Size the remaining per-channel synth arrays to the channel count,
	// preserving existing values and seeding new channels with sane defaults.
	c.viper.Synthesizer.EnableEq = resizeBoolChannels(c.viper.Synthesizer.EnableEq, numChannels, false)
	c.viper.Synthesizer.ChannelMute = resizeBoolChannels(c.viper.Synthesizer.ChannelMute, numChannels, false)
	c.viper.Synthesizer.ChannelGain = resizeGainChannels(c.viper.Synthesizer.ChannelGain, numChannels, defaultChannelGain)
	c.viper.Synthesizer.ChannelName = resizeStringChannels(c.viper.Synthesizer.ChannelName, numChannels, "")

	c.normaliseRouting(numChannels)

	// Compute the EQ curve for each channel
	for ch := range c.viper.Synthesizer.EqBands {
		c.computeEqCurve(ch)
	}

	// Update pulse width extents (inline since we hold the lock)
	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMaxFrequencyHz)
	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.InternalSampleRateHz) /
		(2 * c.viper.Haptics.PulseMinFrequencyHz)

	c.mu.Unlock()

	c.updateJerkScale()
	c.updateSnapScale()
}

// updateJerkScale recalculates the jerk scale factor from the current jerk curve
// and pivot pair.
//
// The response is amplitude(jerk) = scale * jerk^exponent, anchored so the pivot
// jerk sits pivotGain dB below full scale:
//
//	scale = 10^(pivotGain/20) / pivot^exponent
//
// The full-scale jerk is then implied rather than configured, at
// pivot * 10^(-pivotGain/(20*exponent)) — it recedes as the curve flattens, which
// is the point: the reference event holds its level while the shape changes
// around it.
func (c *Config) updateJerkScale() {
	c.mu.Lock()
	exponent := float64(c.viper.Haptics.JerkCurve) / 1000.0
	pivot := float64(c.viper.Haptics.JerkPivot)
	pivotGain := math.Pow(10, c.viper.Haptics.JerkPivotGain/20)
	c.viper.Haptics._jerkScale = pivotGain / math.Pow(pivot, exponent)
	c.rebuildSnapshot()
	c.registerUpdate(false)
	c.mu.Unlock()
}

// migrateJerkMax converts a surviving jerkMax value into the equivalent pivot and
// then clears it, so the conversion runs at most once and omitempty drops the key
// on the next write.
//
// jerkMax named the full-scale jerk directly, so the pivot that reproduces the
// same curve is the jerk at which that curve has fallen to the pivot gain:
//
//	pivot = jerkMax * 10^(pivotGain/(20*exponent))
//
// Caller must hold c.mu.
func (c *Config) migrateJerkMax() {
	if c.viper.Haptics.JerkMax <= 0 {
		return
	}

	exponent := float64(c.viper.Haptics.JerkCurve) / 1000.0
	if exponent <= 0 {
		c.viper.Haptics.JerkMax = 0

		return
	}

	// jerkMax counted in hundreds of m/s^3; the pivot is a plain m/s^3 figure.
	jerkMax := 100 * float64(c.viper.Haptics.JerkMax)
	pivotGain := c.viper.Haptics.JerkPivotGain / 20
	pivot := jerkMax * math.Pow(10, pivotGain/exponent)

	c.viper.Haptics.JerkPivot = min(hapticsJerkPivotMax, max(hapticsJerkPivotMin, int(math.Round(pivot))))
	c.viper.Haptics.JerkMax = 0
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

// computeEqCurve computes the EQ curve for a specific channel based on its bands.
// Uses 8-band parametric EQ with bell filters.
func (c *Config) computeEqCurve(channel int) {
	const (
		minFreqHz    = 5.0
		maxFreqHz    = 160.0
		resolutionHz = 0.5
	)

	if channel < 0 || channel >= len(c.viper.Synthesizer.EqBands) {
		return
	}

	numBuckets := int((maxFreqHz-minFreqHz)/resolutionHz) + 1
	curve := make([]float64, numBuckets)

	// For each frequency bucket, compute the EQ response using bell filter
	for bucketNum := range numBuckets {
		freq := minFreqHz + float64(bucketNum)*resolutionHz

		// Start with unity gain (1.0 in linear, 0.0 in dB)
		amplitudeRatio := 1.0

		// Apply each band's bell filter by multiplication in linear space
		for _, band := range c.viper.Synthesizer.EqBands[channel] {
			amplitudeRatio *= bellFilterResponse(freq, band)
		}

		// Store the final amplitude ratio for efficient multiplication
		curve[bucketNum] = amplitudeRatio
	}

	// Find the deepest attenuation (lowest ratio below 1.0) for DRX headroom
	deepestRatio := 1.0
	for _, ratio := range curve {
		if ratio < deepestRatio {
			deepestRatio = ratio
		}
	}

	// Convert to dB: 0 dB means no headroom, negative values indicate available headroom
	drxHeadroomDB := 20 * math.Log10(deepestRatio)

	// Ensure the curves slice is large enough
	for len(c.viper.Synthesizer._eqCurve) <= channel {
		c.viper.Synthesizer._eqCurve = append(c.viper.Synthesizer._eqCurve, nil)
	}

	for len(c.viper.Synthesizer._drxHeadroom) <= channel {
		c.viper.Synthesizer._drxHeadroom = append(c.viper.Synthesizer._drxHeadroom, 0.0)
	}

	c.viper.Synthesizer._eqCurve[channel] = curve
	c.viper.Synthesizer._drxHeadroom[channel] = drxHeadroomDB
	c.viper.Synthesizer._eqMinFreq = minFreqHz
	c.viper.Synthesizer._eqMaxFreq = maxFreqHz
	c.viper.Synthesizer._eqResolution = resolutionHz
}

// bellFilterResponse returns the amplitude ratio contribution of a single EQ band
// at the given frequency. Returns 1.0 (unity) if the band has no effect.
//
// Uses bell filter magnitude response: H(f) = G / sqrt(1 + Q² × (f/fc - fc/f)²).
func bellFilterResponse(freq float64, band EQBand) float64 {
	if band.Gain == 0.0 {
		return 1.0
	}

	qFactor := band.Q
	if qFactor <= 0 {
		qFactor = 2.0
	}

	freqRatio := freq / band.Frequency
	if freqRatio <= 0 {
		return 1.0
	}

	delta := freqRatio - 1.0/freqRatio
	denom := math.Sqrt(1.0 + qFactor*qFactor*delta*delta)

	if denom <= 0 {
		return 1.0
	}

	bandGainDB := band.Gain / denom

	return math.Pow(10, bandGainDB/20)
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
	// Copy channel arrays
	channelMute := make([]bool, len(c.viper.Synthesizer.ChannelMute))
	copy(channelMute, c.viper.Synthesizer.ChannelMute)

	channelGain := make([]float64, len(c.viper.Synthesizer.ChannelGain))
	copy(channelGain, c.viper.Synthesizer.ChannelGain)

	channelName := make([]string, len(c.viper.Synthesizer.ChannelName))
	copy(channelName, c.viper.Synthesizer.ChannelName)

	newSnap := &Snapshot{
		MasterMute:                c.viper.Synthesizer.MasterMute,
		MasterGain:                c.viper.Synthesizer.MasterGain,
		ChannelMute:               channelMute,
		ChannelGain:               channelGain,
		ChannelName:               channelName,
		ChassisMute:               c.viper.Synthesizer.ChassisMute,
		ChassisGain:               c.viper.Synthesizer.ChassisGain,
		TextureMute:               c.viper.Synthesizer.TextureMute,
		TextureGain:               c.viper.Synthesizer.TextureGain,
		TransmissionMute:          c.viper.Synthesizer.TransmissionMute,
		TransmissionGain:          c.viper.Synthesizer.TransmissionGain,
		TransmissionGainMinRace:   c.viper.Synthesizer.TransmissionGainMinRace,
		TransmissionGainMinStreet: c.viper.Synthesizer.TransmissionGainMinStreet,
		EngineMute:                c.viper.Synthesizer.EngineMute,
		EngineGain:                c.viper.Synthesizer.EngineGain,
		GainIncrement:             c.viper.Synthesizer.GainIncrement,
		InternalSampleRateHz:      c.viper.Synthesizer.InternalSampleRateHz,

		JerkCurve:     float64(c.viper.Haptics.JerkCurve),
		JerkPivot:     c.viper.Haptics.JerkPivot,
		JerkPivotGain: c.viper.Haptics.JerkPivotGain,
		JerkScale:     c.viper.Haptics._jerkScale,

		SnapCurve: float64(c.viper.Haptics.SnapCurve),
		SnapMax:   c.viper.Haptics.SnapMax,
		SnapScale: c.viper.Haptics._snapScale,

		PulseMaxAmplitude:   c.viper.Haptics.PulseMaxAmplitude,
		PulseMaxFrequencyHz: c.viper.Haptics.PulseMaxFrequencyHz,
		PulseMinFrequencyHz: c.viper.Haptics.PulseMinFrequencyHz,
		PulseWidthMin:       c.viper.Haptics._pulseWidthMin,
		PulseWidthMax:       c.viper.Haptics._pulseWidthMax,

		TextureMinFrequencyHz: c.viper.Haptics.TextureMinFrequencyHz,
		TextureMaxFrequencyHz: c.viper.Haptics.TextureMaxFrequencyHz,

		DRXEnabled: c.viper.Synthesizer.EnableDRX,

		DynamicTransmissionFeedback:  c.viper.Haptics.DynamicTransmissionFeedback,
		DynamicTransmissionJerkCurve: c.viper.Haptics.DynamicTransmissionJerkCurve,
		DynamicTransmissionStepBlend: c.viper.Haptics.DynamicTransmissionStepBlend,

		EqEnabled: func() []bool {
			eqEnabled := make([]bool, len(c.viper.Synthesizer.EnableEq))
			copy(eqEnabled, c.viper.Synthesizer.EnableEq)

			return eqEnabled
		}(),

		Routing: func() map[string][]bool {
			routing := make(map[string][]bool, len(c.viper.Synthesizer.Routing))
			for source, row := range c.viper.Synthesizer.Routing {
				dup := make([]bool, len(row))
				copy(dup, row)
				routing[source] = dup
			}

			return routing
		}(),

		FuelMonitoringEnabled: c.viper.PitRadio.FuelMonitoring.Enabled,
		TyreMonitoringEnabled: c.viper.PitRadio.TyreMonitoring.Enabled,

		DisplayOrientation: c.viper.Hardware.DisplayOrientation,
	}
	c.snapshot.Store(newSnap)
}

// syncEngineProfileToMap copies the current engine profile pointer back to the map.
// This must be called after modifying _engineProfile to ensure changes are persisted.
// Caller must hold the mutex.
func (c *Config) syncEngineProfileToMap() {
	if c.viper.Haptics._engineProfile != nil && c.viper.Haptics._engineProfileName != "" {
		c.viper.Haptics.EngineProfiles[c.viper.Haptics._engineProfileName] = *c.viper.Haptics._engineProfile
	}
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
