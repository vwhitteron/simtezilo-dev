package config

import (
	"math"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/vwhitteron/simtezilo-dev/app/i18n"
)

type App struct {
	Language   string
	LogLevel   string
	ReplayMode bool
}

type Hardware struct {
	Model              string
	DisplayOrientation int
}

type Synthesizer struct {
	DynamicTransmissionFeedback  bool
	DynamicTransmissionCurve     int
	DynamicTransmissionGforceMax float64
	SampleRateHz                 int
	OutputFile                   string
	JerkCurve                    int
	JerkMax                      int
	JerkScale                    float64
	SnapCurve                    int
	SnapMax                      int
	SnapScale                    float64
	PulseMaxAmplitude            float64
	PulseMaxFrequencyHz          float64
	PulseMinFrequencyHz          float64
	PulseWidthMax                float64
	PulseWidthMin                float64
	Algorithm                    string
	MasterGain                   float64
	ChassisGain                  float64
	TransmissionGain             float64
	TransmissionGainMinRace      float64
	TransmissionGainMinStreet    float64
	EngineGain                   float64
	EngineFrequencyMin           float64
	EngineFrequencyMax           float64
	GainIncrement                float64
	Eq                           []float64
}

type Telemetry struct {
	Source string
}

type Config struct {
	App         *App
	Hardware    *Hardware
	Synthesizer *Synthesizer
	Telemetry   *Telemetry
	mu          sync.RWMutex
}

func NewConfig(filename string, log zerolog.Logger) *Config {
	c := &Config{
		App: &App{
			Language: "en",
			LogLevel: "info",
		},
		Hardware: &Hardware{
			Model:              "none",
			DisplayOrientation: 0,
		},
		Synthesizer: &Synthesizer{
			SampleRateHz:                 8000,
			OutputFile:                   "",
			DynamicTransmissionFeedback:  true,
			DynamicTransmissionCurve:     150,
			DynamicTransmissionGforceMax: 1.0,
			JerkCurve:                    375,
			JerkMax:                      50,
			SnapCurve:                    420,
			SnapMax:                      52,
			PulseMaxAmplitude:            1,
			PulseMaxFrequencyHz:          60,
			PulseMinFrequencyHz:          16,
			PulseWidthMax:                0.5,
			PulseWidthMin:                0.1,
			Algorithm:                    "sum",
			MasterGain:                   -15,
			GainIncrement:                0.25,
			ChassisGain:                  0,
			TransmissionGain:             -2,
			TransmissionGainMinRace:      -7,
			TransmissionGainMinStreet:    -10,
			EngineGain:                   -8,
			EngineFrequencyMin:           15,
			EngineFrequencyMax:           45,
			Eq: []float64{
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 10-19Hz
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 20-29Hz
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 30-39Hz
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 40-49Hz
			},
		},
		Telemetry: &Telemetry{
			Source: "udp://255.255.255.255:33739",
		},
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
		err = viper.Unmarshal(c)
		if err != nil {
			log.Error().Err(err).Msg("unmarshal config")
		}
	}

	configSource := viper.ConfigFileUsed()
	if configSource == "" {
		configSource = "internal default"
	}

	log.Debug().Str("source", configSource).Msg("config loaded")

	if len(c.Synthesizer.Eq) != 40 {
		log.Warn().Int("length", len(c.Synthesizer.Eq)).Msg("invalid synthesizer EQ length")

		c.Synthesizer.Eq = make([]float64, 40)
		for i := 0; i < 40; i++ {
			c.Synthesizer.Eq[i] = 1
		}
	}

	c.Synthesizer.PulseWidthMin = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMaxFrequencyHz)
	c.Synthesizer.PulseWidthMax = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMinFrequencyHz)

	c.UpdateJerkScale()
	c.UpdateSnapScale()

	return c
}

func (c *Config) IncreaseMasterGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.MasterGain < -c.Synthesizer.GainIncrement {
		c.Synthesizer.MasterGain += c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.MasterGain = MaximumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.MasterGain
}

func (c *Config) DecreaseMasterGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.MasterGain > MinimumGain+c.Synthesizer.GainIncrement {
		c.Synthesizer.MasterGain -= c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.MasterGain = MinimumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.MasterGain
}

func (c *Config) GetMasterGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.MasterGain
}

func (c *Config) IncreaseChassisGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.ChassisGain < -c.Synthesizer.GainIncrement {
		c.Synthesizer.ChassisGain += c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.ChassisGain = MaximumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.ChassisGain
}

func (c *Config) DecreaseChassisGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.ChassisGain > MinimumGain+c.Synthesizer.GainIncrement {
		c.Synthesizer.ChassisGain -= c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.ChassisGain = MinimumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.ChassisGain
}

func (c *Config) GetChassisGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.ChassisGain
}

func (c *Config) IncreaseTransmissionGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.TransmissionGain < -c.Synthesizer.GainIncrement {
		c.Synthesizer.TransmissionGain += c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.TransmissionGain = MaximumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.TransmissionGain
}

func (c *Config) DecreaseTransmissionGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.TransmissionGain > MinimumGain+c.Synthesizer.GainIncrement {
		c.Synthesizer.TransmissionGain -= c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.TransmissionGain = MinimumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.TransmissionGain
}

func (c *Config) GetTransmissionGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.TransmissionGain
}

func (c *Config) DynamicTransmissionFeedbackEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.DynamicTransmissionFeedback
}

func (c *Config) GetJerkCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.Synthesizer.JerkCurve) / 1000.0
}

func (c *Config) GetJerkScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.JerkScale
}

func (c *Config) GetSnapCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.Synthesizer.SnapCurve) / 1000.0
}

func (c *Config) GetTransmissionCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.Synthesizer.DynamicTransmissionCurve) / 1000
}

func (c *Config) GetTransmissionGforceMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.DynamicTransmissionGforceMax
}

func (c *Config) GetEngineGain() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.EngineGain
}

func (c *Config) GetEngineFrequencyMin() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.EngineFrequencyMin
}

func (c *Config) GetEngineFrequencyMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.EngineFrequencyMax
}

func (c *Config) IncreaseEngineGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.EngineGain < -c.Synthesizer.GainIncrement {
		c.Synthesizer.EngineGain += c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.EngineGain = MaximumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.EngineGain
}

func (c *Config) DecreaseEngineGain() float64 {
	c.mu.Lock()

	if c.Synthesizer.EngineGain > MinimumGain+c.Synthesizer.GainIncrement {
		c.Synthesizer.EngineGain -= c.Synthesizer.GainIncrement
	} else {
		c.Synthesizer.EngineGain = MinimumGain
	}

	c.mu.Unlock()

	return c.Synthesizer.EngineGain
}

func (c *Config) GetSnapScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.SnapScale
}

func (c *Config) GetJerkMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.JerkMax
}

func (c *Config) GetSnapMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.SnapMax
}

func (c *Config) DecreaseJerkCurve() int {
	c.mu.Lock()

	c.Synthesizer.JerkCurve -= 5
	if c.Synthesizer.JerkCurve < 5 {
		c.Synthesizer.JerkCurve = 5
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkCurve
}

func (c *Config) DecreaseJerkMax() int {
	c.mu.Lock()

	if c.Synthesizer.JerkMax > 1 {
		c.Synthesizer.JerkMax--
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkMax
}

func (c *Config) IncreaseJerkCurve() int {
	c.mu.Lock()

	if c.Synthesizer.JerkCurve <= 950 {
		c.Synthesizer.JerkCurve += 5
	} else {
		c.Synthesizer.JerkCurve = 955
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkCurve
}

func (c *Config) IncreaseJerkMax() int {
	c.mu.Lock()

	if c.Synthesizer.JerkMax <= 99 {
		c.Synthesizer.JerkMax++
	} else {
		c.Synthesizer.JerkMax = 100
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkMax
}

func (c *Config) UpdateJerkScale() {
	exponent := c.GetJerkCurve()
	jerkMax := 100 * float64(c.Synthesizer.JerkMax)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.Synthesizer.JerkScale = 1 / math.Pow(jerkMax, exponent)
}

func (c *Config) DecreaseSnapCurve() int {
	c.mu.Lock()

	if c.Synthesizer.SnapCurve >= 10 {
		c.Synthesizer.SnapCurve -= 5
	} else {
		c.Synthesizer.SnapCurve = 5
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapCurve
}

func (c *Config) DecreaseSnapMax() int {
	c.mu.Lock()

	if c.Synthesizer.SnapMax > 1 {
		c.Synthesizer.SnapMax--
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapMax
}

func (c *Config) IncreaseSnapCurve() int {
	c.mu.Lock()

	if c.Synthesizer.SnapCurve <= 950 {
		c.Synthesizer.SnapCurve += 5
	} else {
		c.Synthesizer.SnapCurve = 955
	}
	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapCurve
}

func (c *Config) IncreaseSnapMax() int {
	c.mu.Lock()

	if c.Synthesizer.SnapMax <= 99 {
		c.Synthesizer.SnapMax++
	} else {
		c.Synthesizer.SnapMax = 100
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapMax
}

func (c *Config) UpdateSnapScale() {
	exponent := c.GetSnapCurve()
	snapMax := 1000 * float64(c.Synthesizer.SnapMax)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.Synthesizer.SnapScale = 1 / math.Pow(snapMax, exponent)
}

func (c *Config) GetMinHz() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.PulseMinFrequencyHz
}

func (c *Config) IncreaseMinHz() int {
	c.mu.Lock()

	if c.Synthesizer.PulseMinFrequencyHz <= 24 {
		c.Synthesizer.PulseMinFrequencyHz += 1
	} else {
		c.Synthesizer.PulseMinFrequencyHz = 25
	}

	c.Synthesizer.PulseWidthMax = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMinFrequencyHz)

	c.mu.Unlock()

	return int(c.Synthesizer.PulseMinFrequencyHz)
}

func (c *Config) DecreaseMinHz() int {
	c.mu.Lock()

	if c.Synthesizer.PulseMinFrequencyHz >= 5 {
		c.Synthesizer.PulseMinFrequencyHz -= 1
	} else {
		c.Synthesizer.PulseMinFrequencyHz = 5
	}

	c.Synthesizer.PulseWidthMax = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMinFrequencyHz)

	c.mu.Unlock()

	return int(c.Synthesizer.PulseMinFrequencyHz)
}

func (c *Config) GetMaxHz() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.PulseMaxFrequencyHz
}

func (c *Config) IncreaseMaxHz() int {
	c.mu.Lock()

	if c.Synthesizer.PulseMaxFrequencyHz <= 99 {
		c.Synthesizer.PulseMaxFrequencyHz += 1
	} else {
		c.Synthesizer.PulseMaxFrequencyHz = 100
	}

	c.Synthesizer.PulseWidthMin = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMaxFrequencyHz)

	c.mu.Unlock()

	return int(c.Synthesizer.PulseMaxFrequencyHz)
}

func (c *Config) DecreaseMaxHz() int {
	c.mu.Lock()

	if c.Synthesizer.PulseMaxFrequencyHz >= 26 {
		c.Synthesizer.PulseMaxFrequencyHz -= 1
	} else {
		c.Synthesizer.PulseMaxFrequencyHz = 25
	}

	c.Synthesizer.PulseWidthMin = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMaxFrequencyHz)

	c.mu.Unlock()

	return int(c.Synthesizer.PulseMaxFrequencyHz)
}

func (c *Config) IncreaseTransmissionCurve() int {
	c.mu.Lock()

	if c.Synthesizer.DynamicTransmissionCurve <= 950 {
		c.Synthesizer.DynamicTransmissionCurve += 5
	} else {
		c.Synthesizer.DynamicTransmissionCurve = 955
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicTransmissionCurve
}

func (c *Config) DecreaseTransmissionCurve() int {
	c.mu.Lock()

	if c.Synthesizer.DynamicTransmissionCurve >= 10 {
		c.Synthesizer.DynamicTransmissionCurve -= 5
	} else {
		c.Synthesizer.DynamicTransmissionCurve = 5
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicTransmissionCurve
}

func (c *Config) IncreaseTransmissionGforceMax() float64 {
	c.mu.Lock()

	if c.Synthesizer.DynamicTransmissionGforceMax <= 4.9 {
		c.Synthesizer.DynamicTransmissionGforceMax += 0.1
	} else {
		c.Synthesizer.DynamicTransmissionGforceMax = 5.0
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicTransmissionGforceMax
}

func (c *Config) DecreaseTransmissionGforceMax() float64 {
	c.mu.Lock()

	if c.Synthesizer.DynamicTransmissionGforceMax >= 0.2 {
		c.Synthesizer.DynamicTransmissionGforceMax -= 0.1
	} else {
		c.Synthesizer.DynamicTransmissionGforceMax = 0.1
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicTransmissionGforceMax
}

func (c *Config) NextLanguage() string {
	languageCodes := i18n.GetLanguageCodes()

	language := languageCodes[0]
	for i, lang := range languageCodes {
		if lang == c.App.Language {
			nextIndex := (i + 1) % len(languageCodes)
			language = languageCodes[nextIndex]

			break
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.App.Language = language

	return c.App.Language
}

func (c *Config) PreviousLanguage() string {
	languageCodes := i18n.GetLanguageCodes()

	language := languageCodes[0]
	for i, lang := range languageCodes {
		if lang == c.App.Language {
			prevIndex := (i - 1 + len(languageCodes)) % len(languageCodes)
			language = languageCodes[prevIndex]

			break
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.App.Language = language

	return c.App.Language
}

func (c *Config) GetLanguage() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.App.Language == "" {
		return "en"
	}

	return c.App.Language
}

func (c *Config) GetFrequencyHzRange() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.PulseMaxFrequencyHz - c.Synthesizer.PulseMinFrequencyHz
}

func (c *Config) GetPulseWidthMin() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.PulseWidthMin
}

func (c *Config) GetPulseWidthMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.PulseWidthMax
}
