package config

import (
	"math"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

type App struct {
	AssetDir   string
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
	JerkExponent float64
	JerkScale    float64
	SnapExponent float64
	SnapScale    float64
}

type Synthesizer struct {
	SampleRateHz        int
	OutputFile          string
	Profiles            []SynthProfile
	ForceProfile        int
	ForceMax            int
	ForceScale          float64
	GrainProfile        int
	GrainMax            int
	GrainScale          float64
	PulseMaxAmplitude   float64
	PulseMaxFrequencyHz float64
	PulseMinFrequencyHz float64
	PulseWidthMax       float64
	PulseWidthMin       float64
	Algorithm           string
	MasterGain          float64
	GainIncrement       float64
	ChassisVolume       int
	GearRaceVolume      int
	GearStreetVolume    int
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
			AssetDir: "assets",
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
				{JerkExponent: 0.475, JerkScale: 0.01748, SnapExponent: 0.475, SnapScale: 0.00455},
				{JerkExponent: 0.450, JerkScale: 0.02168, SnapExponent: 0.450, SnapScale: 0.00618},
				{JerkExponent: 0.425, JerkScale: 0.02681, SnapExponent: 0.425, SnapScale: 0.00838},
				{JerkExponent: 0.400, JerkScale: 0.03312, SnapExponent: 0.400, SnapScale: 0.01137},
				{JerkExponent: 0.375, JerkScale: 0.04098, SnapExponent: 0.375, SnapScale: 0.01543},
				{JerkExponent: 0.350, JerkScale: 0.05077, SnapExponent: 0.350, SnapScale: 0.02093},
				{JerkExponent: 0.325, JerkScale: 0.06280, SnapExponent: 0.325, SnapScale: 0.02840},
				{JerkExponent: 0.300, JerkScale: 0.07768, SnapExponent: 0.300, SnapScale: 0.03853},
				{JerkExponent: 0.275, JerkScale: 0.09614, SnapExponent: 0.275, SnapScale: 0.05228},
				{JerkExponent: 0.250, JerkScale: 0.11895, SnapExponent: 0.250, SnapScale: 0.07094},
			},
			ForceProfile:        5,
			ForceMax:            50,
			GrainProfile:        5,
			GrainMax:            52,
			PulseMaxAmplitude:   1,
			PulseMaxFrequencyHz: 60,
			PulseMinFrequencyHz: 16,
			PulseWidthMax:       0.5,
			PulseWidthMin:       0.1,
			Algorithm:           "sum",
			MasterGain:          -15,
			GainIncrement:       0.25,
			ChassisVolume:       100,
			GearRaceVolume:      100,
			GearStreetVolume:    50,
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
	profile := c.GetJerkProfile()

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.Profiles[profile-1].JerkExponent
}

func (c *Config) GetJerkScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.ForceScale
}

func (c *Config) GetSnapExponent() float64 {
	profile := c.GetSnapProfile()

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.Profiles[profile-1].SnapExponent
}

func (c *Config) GetSnapScale() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.GrainScale
}

func (c *Config) GetJerkProfile() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.ForceProfile
}

func (c *Config) GetJerkMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.ForceMax
}

func (c *Config) GetSnapProfile() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.GrainProfile
}

func (c *Config) GetSnapMax() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.Synthesizer.GrainMax
}

func (c *Config) PreviousJerkProfile() int {
	c.mu.Lock()

	if c.Synthesizer.ForceProfile > 1 {
		c.Synthesizer.ForceProfile--
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.ForceProfile
}

func (c *Config) NextJerkProfile() int {
	c.mu.Lock()

	if c.Synthesizer.ForceProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.ForceProfile++
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.ForceProfile
}

func (c *Config) DecreaseJerkMax() int {
	c.mu.Lock()

	if c.Synthesizer.ForceMax > 1 {
		c.Synthesizer.ForceMax--
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.ForceMax
}

func (c *Config) IncreaseJerkMax() int {
	c.mu.Lock()

	if c.Synthesizer.ForceMax < 100 {
		c.Synthesizer.ForceMax++
	}

	c.mu.Unlock()

	c.UpdateJerkScale()

	return c.Synthesizer.ForceMax
}

func (c *Config) UpdateJerkScale() {
	exponent := c.GetJerkExponent()
	forceMax := 100 * float64(c.Synthesizer.ForceMax)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.Synthesizer.ForceScale = 1 / math.Pow(forceMax, exponent)
}

func (c *Config) PreviousSnapProfile() int {
	c.mu.Lock()

	if c.Synthesizer.GrainProfile > 1 {
		c.Synthesizer.GrainProfile--
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.GrainProfile
}

func (c *Config) NextSnapProfile() int {
	c.mu.Lock()

	if c.Synthesizer.GrainProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.GrainProfile++
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.GrainProfile
}

func (c *Config) DecreaseSnapMax() int {
	c.mu.Lock()

	if c.Synthesizer.GrainMax > 1 {
		c.Synthesizer.GrainMax--
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.GrainMax
}

func (c *Config) IncreaseSnapMax() int {
	c.mu.Lock()

	if c.Synthesizer.GrainMax < 100 {
		c.Synthesizer.GrainMax++
	}

	c.mu.Unlock()

	c.UpdateSnapScale()

	return c.Synthesizer.GrainMax
}

func (c *Config) UpdateSnapScale() {
	exponent := c.GetSnapExponent()
	grainMax := 1000 * float64(c.Synthesizer.GrainMax)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.Synthesizer.GrainScale = 1 / math.Pow(grainMax, exponent)
}
