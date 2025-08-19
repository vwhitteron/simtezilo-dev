package config

import (
	"math"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	appHaptics "github.com/vwhitteron/simtezilo-dev/app/haptics"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
)

type app struct {
	Language   string
	LogLevel   string
	ReplayMode bool
}

type hardware struct {
	Model              string
	DisplayOrientation int
}

type Synthesizer struct {
	SampleRateHz              int
	OutputFile                string
	MasterGain                float64
	ChassisGain               float64
	TransmissionGain          float64
	TransmissionGainMinRace   float64
	TransmissionGainMinStreet float64
	EngineGain                float64
	GainIncrement             float64
	Eq                        []float64
}

type haptics struct {
	dynamicTransmissionFeedback  bool
	dynamicTransmissionCurve     int
	dynamicTransmissionGforceMax float64
	jerkCurve                    int
	jerkMax                      int
	jerkScale                    float64
	snapCurve                    int
	snapMax                      int
	_snapScale                   float64
	pulseMaxAmplitude            float64
	pulseMaxFrequencyHz          float64
	pulseMinFrequencyHz          float64
	_pulseWidthMax               float64
	_pulseWidthMin               float64
	engineFrequencyMin           float64 // TODO: unused, probably remove
	engineFrequencyMax           float64 // TODO: unused, probably remove
	engineProfiles               map[string]appHaptics.EngineProfile
	_engineProfile               *appHaptics.EngineProfile
}

type Telemetry struct {
	Source string
}

type viperConfig struct {
	App         *app
	Hardware    *hardware
	Haptics     *haptics
	Synthesizer *Synthesizer
	Telemetry   *Telemetry
}

// Viper structs are public by default so make them private and require methods to access them]
type Config struct {
	viper *viperConfig
	mu    sync.RWMutex
}

func NewConfig(filename string, log zerolog.Logger) *Config {
	c := &Config{
		viper: &defaultConfig,
	}

	viper.SetEnvPrefix("SIMTEZILO")
	viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))
	viper.AutomaticEnv()
	viper.SetConfigName(filename)
	viper.SetConfigType("toml")
	viper.AddConfigPath("/boot/simtezilo/")
	viper.AddConfigPath("/opt/simtezilo/")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		log.Error().Err(err).Msg("read config file")
	} else {
		err = viper.Unmarshal(c.viper)
		if err != nil {
			log.Error().Err(err).Msg("unmarshal config")
		}
	}

	configSource := viper.ConfigFileUsed()
	if configSource == "" {
		configSource = "internal default"
	}

	log.Debug().Str("source", configSource).Msg("config loaded")

	if len(c.viper.Synthesizer.Eq) != 40 {
		log.Warn().Int("length", len(c.viper.Synthesizer.Eq)).Msg("invalid synthesizer EQ length")

		c.viper.Synthesizer.Eq = make([]float64, 40)
		for i := 0; i < 40; i++ {
			c.viper.Synthesizer.Eq[i] = 1
		}
	}

	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.SampleRateHz) / (2 * c.viper.Haptics.pulseMaxFrequencyHz)
	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.SampleRateHz) / (2 * c.viper.Haptics.pulseMinFrequencyHz)

	c.UpdateJerkScale()
	c.UpdateSnapScale()

	return c
}

// App methods
func (c *Config) GetAppLanguage() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.App.Language == "" {
		return "en"
	}

	return c.viper.App.Language
}

func (c *Config) NextLanguage() string {
	languageCodes := i18n.GetLanguageCodes()

	language := languageCodes[0]
	for i, lang := range languageCodes {
		if lang == c.viper.App.Language {
			nextIndex := (i + 1) % len(languageCodes)
			language = languageCodes[nextIndex]

			break
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.Language = language

	return c.viper.App.Language
}

func (c *Config) PreviousLanguage() string {
	languageCodes := i18n.GetLanguageCodes()

	language := languageCodes[0]
	for i, lang := range languageCodes {
		if lang == c.viper.App.Language {
			prevIndex := (i - 1 + len(languageCodes)) % len(languageCodes)
			language = languageCodes[prevIndex]

			break
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.App.Language = language

	return c.viper.App.Language
}

func (c *Config) GetAppLogLevel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.App.LogLevel == "" {
		return "info"
	}

	return c.viper.App.LogLevel
}

func (c *Config) GetAppReplayMode() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.App.ReplayMode
}

// Hardware methods
func (c *Config) GetHardwareModel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Hardware.Model
}

func (c *Config) GetDisplayOrientation() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Hardware.DisplayOrientation
}

// Synthesizer methods
func (c *Config) GetSynthesizer() *Synthesizer {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer
}

func (c *Config) GetSampleRateHz() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.SampleRateHz
}

func (c *Config) GetMasterGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.MasterGain
}

func (c *Config) IncreaseMasterGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.MasterGain < -c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.MasterGain += c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.MasterGain = MaximumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.MasterGain
}

func (c *Config) DecreaseMasterGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.MasterGain > MinimumGain+c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.MasterGain -= c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.MasterGain = MinimumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.MasterGain
}

func (c *Config) GetChassisGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.ChassisGain
}

func (c *Config) IncreaseChassisGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.ChassisGain < -c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.ChassisGain += c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.ChassisGain = MaximumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.ChassisGain
}

func (c *Config) GetTransmissionGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.TransmissionGain
}

func (c *Config) DecreaseChassisGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.ChassisGain > MinimumGain+c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.ChassisGain -= c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.ChassisGain = MinimumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.ChassisGain
}

func (c *Config) IncreaseTransmissionGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.TransmissionGain < -c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.TransmissionGain += c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.TransmissionGain = MaximumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.TransmissionGain
}

func (c *Config) DecreaseTransmissionGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.TransmissionGain > MinimumGain+c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.TransmissionGain -= c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.TransmissionGain = MinimumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.TransmissionGain
}

func (c *Config) GetTransmissionGainMinRace() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.TransmissionGainMinRace
}

func (c *Config) GetTransmissionGainMinStreet() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.TransmissionGainMinStreet
}

func (c *Config) GetEngineGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Synthesizer.EngineGain
}

func (c *Config) IncreaseEngineGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.EngineGain < -c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.EngineGain += c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.EngineGain = MaximumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.EngineGain
}

func (c *Config) DecreaseEngineGain() float64 {
	c.mu.Lock()

	if c.viper.Synthesizer.EngineGain > MinimumGain+c.viper.Synthesizer.GainIncrement {
		c.viper.Synthesizer.EngineGain -= c.viper.Synthesizer.GainIncrement
	} else {
		c.viper.Synthesizer.EngineGain = MinimumGain
	}

	c.mu.Unlock()

	return c.viper.Synthesizer.EngineGain
}

// Haptics methods
func (c *Config) DynamicTransmissionFeedbackEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.dynamicTransmissionFeedback
}

func (c *Config) GetJerkCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.viper.Haptics.jerkCurve) / 1000.0
}

func (c *Config) DecreaseJerkCurve() int {
	c.mu.Lock()

	c.viper.Haptics.jerkCurve -= 5
	if c.viper.Haptics.jerkCurve < 5 {
		c.viper.Haptics.jerkCurve = 5
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.jerkCurve
}

func (c *Config) IncreaseJerkCurve() int {
	c.mu.Lock()

	if c.viper.Haptics.jerkCurve <= 950 {
		c.viper.Haptics.jerkCurve += 5
	} else {
		c.viper.Haptics.jerkCurve = 955
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.jerkCurve
}

func (c *Config) GetJerkScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.jerkScale
}

func (c *Config) UpdateJerkScale() {
	exponent := c.GetJerkCurve()
	jerkMax := 100 * float64(c.viper.Haptics.jerkMax)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics.jerkScale = 1 / math.Pow(jerkMax, exponent)
}

func (c *Config) GetJerkMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.jerkMax
}

func (c *Config) IncreaseJerkMax() int {
	c.mu.Lock()

	if c.viper.Haptics.jerkMax <= 99 {
		c.viper.Haptics.jerkMax++
	} else {
		c.viper.Haptics.jerkMax = 100
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.jerkMax
}

func (c *Config) DecreaseJerkMax() int {
	c.mu.Lock()

	if c.viper.Haptics.jerkMax > 1 {
		c.viper.Haptics.jerkMax--
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.viper.Haptics.jerkMax
}

func (c *Config) GetSnapCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.viper.Haptics.snapCurve) / 1000.0
}

func (c *Config) DecreaseSnapCurve() int {
	c.mu.Lock()

	if c.viper.Haptics.snapCurve >= 10 {
		c.viper.Haptics.snapCurve -= 5
	} else {
		c.viper.Haptics.snapCurve = 5
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.snapCurve
}

func (c *Config) IncreaseSnapCurve() int {
	c.mu.Lock()

	if c.viper.Haptics.snapCurve <= 950 {
		c.viper.Haptics.snapCurve += 5
	} else {
		c.viper.Haptics.snapCurve = 955
	}
	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.snapCurve
}

func (c *Config) GetSnapScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics._snapScale
}

func (c *Config) UpdateSnapScale() {
	exponent := c.GetSnapCurve()
	snapMax := 1000 * float64(c.viper.Haptics.snapMax)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.viper.Haptics._snapScale = 1 / math.Pow(snapMax, exponent)
}

func (c *Config) IncreaseSnapMax() int {
	c.mu.Lock()

	if c.viper.Haptics.snapMax <= 99 {
		c.viper.Haptics.snapMax++
	} else {
		c.viper.Haptics.snapMax = 100
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.snapMax
}

func (c *Config) GetSnapMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.snapMax
}

func (c *Config) DecreaseSnapMax() int {
	c.mu.Lock()

	if c.viper.Haptics.snapMax > 1 {
		c.viper.Haptics.snapMax--
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.viper.Haptics.snapMax
}

func (c *Config) GetTransmissionCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.viper.Haptics.dynamicTransmissionCurve) / 1000
}

func (c *Config) DecreaseTransmissionCurve() int {
	c.mu.Lock()

	if c.viper.Haptics.dynamicTransmissionCurve >= 10 {
		c.viper.Haptics.dynamicTransmissionCurve -= 5
	} else {
		c.viper.Haptics.dynamicTransmissionCurve = 5
	}

	c.mu.Unlock()

	return c.viper.Haptics.dynamicTransmissionCurve
}

func (c *Config) IncreaseTransmissionCurve() int {
	c.mu.Lock()

	if c.viper.Haptics.dynamicTransmissionCurve <= 950 {
		c.viper.Haptics.dynamicTransmissionCurve += 5
	} else {
		c.viper.Haptics.dynamicTransmissionCurve = 955
	}

	c.mu.Unlock()

	return c.viper.Haptics.dynamicTransmissionCurve
}

func (c *Config) GetTransmissionGforceMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.dynamicTransmissionGforceMax
}

func (c *Config) DecreaseTransmissionGforceMax() float64 {
	c.mu.Lock()

	if c.viper.Haptics.dynamicTransmissionGforceMax >= 0.2 {
		c.viper.Haptics.dynamicTransmissionGforceMax -= 0.1
	} else {
		c.viper.Haptics.dynamicTransmissionGforceMax = 0.1
	}

	c.mu.Unlock()

	return c.viper.Haptics.dynamicTransmissionGforceMax
}

func (c *Config) IncreaseTransmissionGforceMax() float64 {
	c.mu.Lock()

	if c.viper.Haptics.dynamicTransmissionGforceMax <= 4.9 {
		c.viper.Haptics.dynamicTransmissionGforceMax += 0.1
	} else {
		c.viper.Haptics.dynamicTransmissionGforceMax = 5.0
	}

	c.mu.Unlock()

	return c.viper.Haptics.dynamicTransmissionGforceMax
}

func (c *Config) GetEngineFrequencyMin() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.engineFrequencyMin
}

func (c *Config) GetEngineFrequencyMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.engineFrequencyMax
}

func (c *Config) GetMinHz() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.pulseMinFrequencyHz
}

func (c *Config) GetEngineProfile(name string) *appHaptics.EngineProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if profile, ok := c.viper.Haptics.engineProfiles[name]; ok {
		c.viper.Haptics._engineProfile = &profile

		return c.viper.Haptics._engineProfile
	}

	return nil
}

func (c *Config) GetEnginePrimaryBalance() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 0.0
	}

	return c.viper.Haptics._engineProfile.PrimaryBalance
}

func (c *Config) IncreaseEnginePrimaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.PrimaryBalance < 1.0 {
		c.viper.Haptics._engineProfile.PrimaryBalance += 0.01
	} else {
		c.viper.Haptics._engineProfile.PrimaryBalance = 1.0
	}

	return c.viper.Haptics._engineProfile.PrimaryBalance
}

func (c *Config) DecreaseEnginePrimaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.PrimaryBalance > 0.0 {
		c.viper.Haptics._engineProfile.PrimaryBalance -= 0.01
	} else {
		c.viper.Haptics._engineProfile.PrimaryBalance = 0.0
	}

	return c.viper.Haptics._engineProfile.PrimaryBalance
}

func (c *Config) GetEngineSecondaryBalance() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 0.0
	}

	return c.viper.Haptics._engineProfile.SecondaryBalance
}

func (c *Config) IncreaseEngineSecondaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.SecondaryBalance < 1.0 {
		c.viper.Haptics._engineProfile.SecondaryBalance += 0.01
	} else {
		c.viper.Haptics._engineProfile.SecondaryBalance = 1.0
	}

	return c.viper.Haptics._engineProfile.SecondaryBalance
}

func (c *Config) DecreaseEngineSecondaryBalance() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.SecondaryBalance > 0.0 {
		c.viper.Haptics._engineProfile.SecondaryBalance -= 0.01
	} else {
		c.viper.Haptics._engineProfile.SecondaryBalance = 0.0
	}

	return c.viper.Haptics._engineProfile.SecondaryBalance
}

func (c *Config) GetEnginePulseMagnitude() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 0.0
	}

	return c.viper.Haptics._engineProfile.Magnitude
}

func (c *Config) IncreaseEnginePulseMagnitude() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.Magnitude < 1.0 {
		c.viper.Haptics._engineProfile.Magnitude += 0.01
	} else {
		c.viper.Haptics._engineProfile.Magnitude = 1.0
	}

	return c.viper.Haptics._engineProfile.Magnitude
}

func (c *Config) DecreaseEnginePulseMagnitude() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.Magnitude > 0.0 {
		c.viper.Haptics._engineProfile.Magnitude -= 0.01
	} else {
		c.viper.Haptics._engineProfile.Magnitude = 0.0
	}

	return c.viper.Haptics._engineProfile.Magnitude
}

func (c *Config) GetEnginePulseScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.viper.Haptics._engineProfile == nil {
		return 0.0
	}

	return c.viper.Haptics._engineProfile.PulseScale
}

func (c *Config) IncreaseEnginePulseScale() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.PulseScale < 1.0 {
		c.viper.Haptics._engineProfile.PulseScale += 0.01
	} else {
		c.viper.Haptics._engineProfile.PulseScale = 1.0
	}

	return c.viper.Haptics._engineProfile.PulseScale
}

func (c *Config) DecreaseEnginePulseScale() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.viper.Haptics._engineProfile.PulseScale > 0.0 {
		c.viper.Haptics._engineProfile.PulseScale -= 0.01
	} else {
		c.viper.Haptics._engineProfile.PulseScale = 0.0
	}

	return c.viper.Haptics._engineProfile.PulseScale
}

func (c *Config) DecreaseMinHz() int {
	c.mu.Lock()

	if c.viper.Haptics.pulseMinFrequencyHz >= 5 {
		c.viper.Haptics.pulseMinFrequencyHz -= 1
	} else {
		c.viper.Haptics.pulseMinFrequencyHz = 5
	}

	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.SampleRateHz) / (2 * c.viper.Haptics.pulseMinFrequencyHz)

	c.mu.Unlock()

	return int(c.viper.Haptics.pulseMinFrequencyHz)
}

func (c *Config) IncreaseMinHz() int {
	c.mu.Lock()

	if c.viper.Haptics.pulseMinFrequencyHz <= 24 {
		c.viper.Haptics.pulseMinFrequencyHz += 1
	} else {
		c.viper.Haptics.pulseMinFrequencyHz = 25
	}

	c.viper.Haptics._pulseWidthMax = float64(c.viper.Synthesizer.SampleRateHz) / (2 * c.viper.Haptics.pulseMinFrequencyHz)

	c.mu.Unlock()

	return int(c.viper.Haptics.pulseMinFrequencyHz)
}

func (c *Config) GetMaxHz() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.pulseMaxFrequencyHz
}

func (c *Config) DecreaseMaxHz() int {
	c.mu.Lock()

	if c.viper.Haptics.pulseMaxFrequencyHz >= 26 {
		c.viper.Haptics.pulseMaxFrequencyHz -= 1
	} else {
		c.viper.Haptics.pulseMaxFrequencyHz = 25
	}

	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.SampleRateHz) / (2 * c.viper.Haptics.pulseMaxFrequencyHz)

	c.mu.Unlock()

	return int(c.viper.Haptics.pulseMaxFrequencyHz)
}

func (c *Config) IncreaseMaxHz() int {
	c.mu.Lock()

	if c.viper.Haptics.pulseMaxFrequencyHz <= 99 {
		c.viper.Haptics.pulseMaxFrequencyHz += 1
	} else {
		c.viper.Haptics.pulseMaxFrequencyHz = 100
	}

	c.viper.Haptics._pulseWidthMin = float64(c.viper.Synthesizer.SampleRateHz) / (2 * c.viper.Haptics.pulseMaxFrequencyHz)

	c.mu.Unlock()

	return int(c.viper.Haptics.pulseMaxFrequencyHz)
}

func (c *Config) GetFrequencyHzRange() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.pulseMaxFrequencyHz - c.viper.Haptics.pulseMinFrequencyHz
}

func (c *Config) GetPulseWidthMin() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics._pulseWidthMin
}

func (c *Config) GetPulseWidthMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics._pulseWidthMax
}

func (c *Config) GetPulseMaxAmplitude() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Haptics.pulseMaxAmplitude
}

// Telemetry methods
func (c *Config) GetTelemetrySource() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.viper.Telemetry.Source
}
