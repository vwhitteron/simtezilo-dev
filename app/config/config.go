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
	JerkExponent int
	JerkMax      int
	SnapExponent int
	SnapMax      int
}

type Synthesizer struct {
	SampleRateHz        int
	OutputFile          string
	Profiles            []SynthProfile
	JerkExponent        int
	JerkMax             int
	JerkProfile         int
	JerkScale           float64
	SnapExponent        int
	SnapMax             int
	SnapProfile         int
	SnapScale           float64
	GearExp             int
	GearMax             float64
	PulseMaxAmplitude   float64
	PulseMaxFrequencyHz float64
	PulseMinFrequencyHz float64
	PulseWidthMax       float64
	PulseWidthMin       float64
	Algorithm           string
	MasterGain          float64
	GainIncrement       float64
	ChassisVolume       int
	GearVolume          int
	GearVolumeMinRace   int
	GearVolumeMinStreet int
	Eq                  []float64
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
			OutputFile:   "default",
			Profiles: []SynthProfile{
				{JerkExponent: 475, JerkMax: 80, SnapExponent: 600, SnapMax: 48},
				{JerkExponent: 450, JerkMax: 80, SnapExponent: 555, SnapMax: 48},
				{JerkExponent: 425, JerkMax: 80, SnapExponent: 510, SnapMax: 48},
				{JerkExponent: 400, JerkMax: 80, SnapExponent: 470, SnapMax: 48},
				{JerkExponent: 375, JerkMax: 80, SnapExponent: 420, SnapMax: 48},
				{JerkExponent: 350, JerkMax: 80, SnapExponent: 380, SnapMax: 48},
				{JerkExponent: 325, JerkMax: 80, SnapExponent: 335, SnapMax: 48},
				{JerkExponent: 300, JerkMax: 80, SnapExponent: 390, SnapMax: 48},
				{JerkExponent: 275, JerkMax: 80, SnapExponent: 345, SnapMax: 48},
				{JerkExponent: 250, JerkMax: 80, SnapExponent: 200, SnapMax: 48},
			},
			JerkExponent:        375,
			JerkMax:             50,
			JerkProfile:         5,
			SnapExponent:        420,
			SnapMax:             52,
			SnapProfile:         5,
			GearExp:             150,
			GearMax:             1.0,
			PulseMaxAmplitude:   1,
			PulseMaxFrequencyHz: 60,
			PulseMinFrequencyHz: 16,
			PulseWidthMax:       0.5,
			PulseWidthMin:       0.1,
			Algorithm:           "sum",
			MasterGain:          -15,
			GainIncrement:       0.25,
			ChassisVolume:       100,
			GearVolume:          100,
			GearVolumeMinRace:   40,
			GearVolumeMinStreet: 30,
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

func (c *Config) GetJerkExponent() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.Synthesizer.JerkExponent) / 1000.0
}

func (c *Config) GetJerkScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.JerkScale
}

func (c *Config) GetSnapExponent() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.Synthesizer.SnapExponent) / 1000.0
}

func (c *Config) GetGearExp() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return float64(c.Synthesizer.GearExp) / 1000
}

func (c *Config) GetGearMax() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.GearMax
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

func (c *Config) DecreaseJerkExponent() int {
	c.mu.Lock()

	c.Synthesizer.JerkExponent -= 5
	if c.Synthesizer.JerkExponent < 5 {
		c.Synthesizer.JerkExponent = 5
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkExponent
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

func (c *Config) IncreaseJerkExponent() int {
	c.mu.Lock()

	if c.Synthesizer.JerkExponent <= 950 {
		c.Synthesizer.JerkExponent += 5
	} else {
		c.Synthesizer.JerkExponent = 955
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.JerkExponent
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
	exponent := c.GetJerkExponent()
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

func (c *Config) DecreaseSnapExponent() int {
	c.mu.Lock()

	if c.Synthesizer.SnapExponent >= 10 {
		c.Synthesizer.SnapExponent -= 5
	} else {
		c.Synthesizer.SnapExponent = 5
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapExponent
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

func (c *Config) IncreaseSnapExponent() int {
	c.mu.Lock()

	if c.Synthesizer.SnapExponent <= 950 {
		c.Synthesizer.SnapExponent += 5
	} else {
		c.Synthesizer.SnapExponent = 955
	}
	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.SnapExponent
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
	exponent := c.GetSnapExponent()
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

func (c *Config) IncreaseGearExp() int {
	c.mu.Lock()

	if c.Synthesizer.GearExp <= 950 {
		c.Synthesizer.GearExp += 5
	} else {
		c.Synthesizer.GearExp = 955
	}

	c.mu.Unlock()

	return c.Synthesizer.GearExp
}

func (c *Config) DecreaseGearExp() int {
	c.mu.Lock()

	if c.Synthesizer.GearExp >= 10 {
		c.Synthesizer.GearExp -= 5
	} else {
		c.Synthesizer.GearExp = 5
	}

	c.mu.Unlock()

	return c.Synthesizer.GearExp
}

func (c *Config) IncreaseGearMax() float64 {
	c.mu.Lock()

	if c.Synthesizer.GearMax <= 4.9 {
		c.Synthesizer.GearMax += 0.1
	} else {
		c.Synthesizer.GearMax = 5.0
	}

	c.mu.Unlock()

	return c.Synthesizer.GearMax
}

func (c *Config) DecreaseGearMax() float64 {
	c.mu.Lock()

	if c.Synthesizer.GearMax >= 0.2 {
		c.Synthesizer.GearMax -= 0.1
	} else {
		c.Synthesizer.GearMax = 0.1
	}

	c.mu.Unlock()

	return c.Synthesizer.GearMax
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
