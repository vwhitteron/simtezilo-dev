package config

import (
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
	Profiles            []SynthProfile
	ForceProfile        int
	GrainProfile        int
	PulseMaxAmplitude   float64
	PulseMaxFrequencyHz float64
	PulseMinFrequencyHz float64
	PulseWidthMax       float64
	PulseWidthMin       float64
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
	mu          sync.Mutex
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
			GrainProfile:        5,
			PulseMaxAmplitude:   1,
			PulseMaxFrequencyHz: 60,
			PulseMinFrequencyHz: 16,
			PulseWidthMax:       0.5,
			PulseWidthMin:       0.1,
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

	// viper.SetDefault("Synthesizer.sampleratehz", 8000)
	// viper.SetDefault("Synthesizer.profiles", []SynthProfile{})
	// viper.SetDefault("Synthesizer.pulseMaxAmplitude", 1)
	// viper.SetDefault("Synthesizer.pulseMaxFrequencyHz", 40)
	// viper.SetDefault("Synthesizer.pulseMinFrequencyHz", 23)
	// viper.SetDefault("Synthesizer.pulseWidthMax", 0.5)
	// viper.SetDefault("Synthesizer.pulseWidthMin", 0.1)
	// viper.SetDefault("Display.gearFontSize", 16)
	// viper.SetDefault("Display.volumeFontSize", 16)

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

	// log.Debug().Interface("config", c).Msg("config loaded")

	c.Synthesizer.PulseWidthMin = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMaxFrequencyHz)
	c.Synthesizer.PulseWidthMax = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMinFrequencyHz)

	return c
}

func (c *Config) GetJerkExponent() float64 {
	profile := c.GetJerkProfile()

	return c.Synthesizer.Profiles[profile-1].JerkExponent
}

func (c *Config) GetJerkScale() float64 {
	profile := c.GetJerkProfile()

	return c.Synthesizer.Profiles[profile-1].JerkScale
}

func (c *Config) GetSnapExponent() float64 {
	profile := c.GetSnapProfile()

	return c.Synthesizer.Profiles[profile-1].SnapExponent
}

func (c *Config) GetSnapScale() float64 {
	profile := c.GetSnapProfile()

	return c.Synthesizer.Profiles[profile-1].SnapScale
}

func (c *Config) GetJerkProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.Synthesizer.ForceProfile
}

func (c *Config) GetSnapProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.Synthesizer.GrainProfile
}

// func (c *Config) SetProfile(profile int) bool {
// 	c.mu.Lock()
// 	defer c.mu.Unlock()

// 	if profile < 1 || profile > 10 {
// 		return false
// 	}

// 	c.Synthesizer.Profile = profile

// 	return true
// }

func (c *Config) PreviousJerkProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Synthesizer.ForceProfile > 1 {
		c.Synthesizer.ForceProfile--
	}

	return c.Synthesizer.ForceProfile
}

func (c *Config) NextJerkProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Synthesizer.ForceProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.ForceProfile++
	}

	return c.Synthesizer.ForceProfile
}

func (c *Config) PreviousSnapProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Synthesizer.GrainProfile > 1 {
		c.Synthesizer.GrainProfile--
	}

	return c.Synthesizer.GrainProfile
}

func (c *Config) NextSnapProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Synthesizer.GrainProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.GrainProfile++
	}

	return c.Synthesizer.GrainProfile
}
