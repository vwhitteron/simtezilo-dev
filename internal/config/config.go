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
	SampleRateHz         int
	Profiles             []SynthProfile
	JerkProfile          int
	SnapProfile          int
	PulseExponent        float64
	PulseScaleAdjustment float64
	PulseMaxAmplitude    float64
	PulseMaxFrequencyHz  float64
	PulseMinFrequencyHz  float64
	PulseWidthMax        float64
	PulseWidthMin        float64
	MasterGain           float64
	GainIncrement        float64
	ChassisVolume        int
	GearRaceVolume       int
	GearStreetVolume     int
	Eq                   []float64
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
				{
					JerkExponent: 0.5,
					JerkScale:    1,
					SnapExponent: 0.5,
					SnapScale:    1,
				},
			},
			JerkProfile:          5,
			SnapProfile:          5,
			PulseExponent:        0.56,
			PulseScaleAdjustment: 1 / 54,
			PulseMaxAmplitude:    1,
			PulseMaxFrequencyHz:  40,
			PulseMinFrequencyHz:  23,
			PulseWidthMax:        0.5,
			PulseWidthMin:        0.1,
			MasterGain:           -15,
			GainIncrement:        0.25,
			ChassisVolume:        100,
			GearRaceVolume:       100,
			GearStreetVolume:     50,
			Eq:                   []float64{},
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
	// viper.SetDefault("Synthesizer.PulseExponent", 0.56)
	// viper.SetDefault("Synthesizer.pulseScaleAdjustment", 1/54)
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

	log.Debug().Interface("config", c).Msg("config loaded")

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

	return c.Synthesizer.JerkProfile
}

func (c *Config) GetSnapProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.Synthesizer.SnapProfile
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

	if c.Synthesizer.JerkProfile > 1 {
		c.Synthesizer.JerkProfile--
	}

	return c.Synthesizer.JerkProfile
}

func (c *Config) NextJerkProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Synthesizer.JerkProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.JerkProfile++
	}

	return c.Synthesizer.JerkProfile
}

func (c *Config) PreviousSnapProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Synthesizer.SnapProfile > 1 {
		c.Synthesizer.SnapProfile--
	}

	return c.Synthesizer.SnapProfile
}

func (c *Config) NextSnapProfile() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.Synthesizer.SnapProfile < len(c.Synthesizer.Profiles) {
		c.Synthesizer.SnapProfile++
	}

	return c.Synthesizer.SnapProfile
}
