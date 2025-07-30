package config

import (
	"math"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

type App struct {
	LogLevel   string
	ReplayMode bool
}

type Display struct {
	GearFontSize   int
	VolumeFontSize int
}

type Hardware struct {
	Model              string
	DisplayOrientation int
}

type SynthProfile struct {
	JerkCurve int
	JerkMax   int
	SnapCurve int
	SnapMax   int
}

type Synthesizer struct {
	DynamicGearShiftFeedback  bool
	DynamicGearShiftCurve     int
	DynamicGearShiftGforceMax float64
	SampleRateHz              int
	OutputFile                string
	Profiles                  []SynthProfile
	JerkCurve                 int
	JerkMax                   int
	JerkProfile               int
	JerkScale                 float64
	SnapCurve                 int
	SnapMax                   int
	SnapProfile               int
	SnapScale                 float64
	PulseMaxAmplitude         float64
	PulseMaxFrequencyHz       float64
	PulseMinFrequencyHz       float64
	PulseWidthMax             float64
	PulseWidthMin             float64
	Algorithm                 string
	MasterGain                float64
	GainIncrement             float64
	ChassisVolume             int
	GearShiftVolume           int
	GearShiftVolumeMinRace    int
	GearShiftVolumeMinStreet  int
	Eq                        []float64
}

type Telemetry struct {
	Source string
}

type Config struct {
	App         App
	Display     Display
	Hardware    Hardware
	Synthesizer Synthesizer
	Telemetry   Telemetry
	mu          sync.RWMutex
}

func NewConfig(filename string, log zerolog.Logger) *Config {
	c := &Config{
		App: App{
			LogLevel: "info",
		},
		Display: Display{
			GearFontSize:   48,
			VolumeFontSize: 20,
		},
		Hardware: Hardware{
			Model:              "none",
			DisplayOrientation: 0,
		},
		Synthesizer: Synthesizer{
			SampleRateHz: 8000,
			OutputFile:   "",
			Profiles: []SynthProfile{
				{JerkCurve: 475, JerkMax: 80, SnapCurve: 600, SnapMax: 48},
				{JerkCurve: 450, JerkMax: 80, SnapCurve: 555, SnapMax: 48},
				{JerkCurve: 425, JerkMax: 80, SnapCurve: 510, SnapMax: 48},
				{JerkCurve: 400, JerkMax: 80, SnapCurve: 470, SnapMax: 48},
				{JerkCurve: 375, JerkMax: 80, SnapCurve: 420, SnapMax: 48},
				{JerkCurve: 350, JerkMax: 80, SnapCurve: 380, SnapMax: 48},
				{JerkCurve: 325, JerkMax: 80, SnapCurve: 335, SnapMax: 48},
				{JerkCurve: 300, JerkMax: 80, SnapCurve: 390, SnapMax: 48},
				{JerkCurve: 275, JerkMax: 80, SnapCurve: 345, SnapMax: 48},
				{JerkCurve: 250, JerkMax: 80, SnapCurve: 200, SnapMax: 48},
			},
			DynamicGearShiftFeedback:  true,
			DynamicGearShiftCurve:     150,
			DynamicGearShiftGforceMax: 1.0,
			JerkCurve:                 375,
			JerkMax:                   50,
			JerkProfile:               5,
			SnapCurve:                 420,
			SnapMax:                   52,
			SnapProfile:               5,
			PulseMaxAmplitude:         1,
			PulseMaxFrequencyHz:       60,
			PulseMinFrequencyHz:       16,
			PulseWidthMax:             0.5,
			PulseWidthMin:             0.1,
			Algorithm:                 "sum",
			MasterGain:                -15,
			GainIncrement:             0.25,
			ChassisVolume:             100,
			GearShiftVolume:           100,
			GearShiftVolumeMinRace:    40,
			GearShiftVolumeMinStreet:  30,
			Eq: []float64{
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 10-19Hz
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 20-29Hz
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 30-39Hz
				1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, 1.00, // 40-49Hz
			},
		},
		Telemetry: Telemetry{
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

func (c *Config) DynamicGearShiftFeedbackEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.DynamicGearShiftFeedback
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

func (c *Config) GetGearShiftCurve() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.Synthesizer.DynamicGearShiftCurve) / 1000
}

func (c *Config) GetGearShiftGforceMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.DynamicGearShiftGforceMax
}

func (c *Config) GetSnapScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.SnapScale
}

func (c *Config) GetJerkProfile() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.JerkProfile
}

func (c *Config) GetJerkMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.JerkMax
}

func (c *Config) GetSnapProfile() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.SnapProfile
}

func (c *Config) GetSnapMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.SnapMax
}

func (c *Config) PreviousJerkProfile() int {
	c.mu.Lock()

	if c.Synthesizer.JerkProfile > 1 {
		c.Synthesizer.JerkProfile--
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkProfile
}

func (c *Config) NextJerkProfile() int {
	c.mu.Lock()

	if c.Synthesizer.JerkProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.JerkProfile++
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkProfile
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

func (c *Config) PreviousSnapProfile() int {
	c.mu.Lock()

	if c.Synthesizer.SnapProfile > 1 {
		c.Synthesizer.SnapProfile--
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapProfile
}

func (c *Config) NextSnapProfile() int {
	c.mu.Lock()

	if c.Synthesizer.SnapProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.SnapProfile++
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapProfile
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

func (c *Config) IncreaseGearShiftCurve() int {
	c.mu.Lock()

	if c.Synthesizer.DynamicGearShiftCurve <= 950 {
		c.Synthesizer.DynamicGearShiftCurve += 5
	} else {
		c.Synthesizer.DynamicGearShiftCurve = 955
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicGearShiftCurve
}

func (c *Config) DecreaseGearShiftCurve() int {
	c.mu.Lock()

	if c.Synthesizer.DynamicGearShiftCurve >= 10 {
		c.Synthesizer.DynamicGearShiftCurve -= 5
	} else {
		c.Synthesizer.DynamicGearShiftCurve = 5
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicGearShiftCurve
}

func (c *Config) IncreaseGearShiftGforceMax() float64 {
	c.mu.Lock()

	if c.Synthesizer.DynamicGearShiftGforceMax <= 4.9 {
		c.Synthesizer.DynamicGearShiftGforceMax += 0.1
	} else {
		c.Synthesizer.DynamicGearShiftGforceMax = 5.0
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicGearShiftGforceMax
}

func (c *Config) DecreaseGearShiftGforceMax() float64 {
	c.mu.Lock()

	if c.Synthesizer.DynamicGearShiftGforceMax >= 0.2 {
		c.Synthesizer.DynamicGearShiftGforceMax -= 0.1
	} else {
		c.Synthesizer.DynamicGearShiftGforceMax = 0.1
	}

	c.mu.Unlock()

	return c.Synthesizer.DynamicGearShiftGforceMax
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
