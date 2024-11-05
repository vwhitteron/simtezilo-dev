package config

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
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

type Synthesizer struct {
	SampleRateHz         int
	PulseExponent        float64
	PulseScaleAdjustment float64
	PulseMaxAmplitude    float64
	PulseMaxFrequencyHz  float64
	PulseMinFrequencyHz  float64
	PulseWidthMax        float64
	PulseWidthMin        float64
	MasterGain           float64
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
}

func NewConfig(filename string) *Config {
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
			Eq:                 []float64{},
		},
		Synthesizer: Synthesizer{
			SampleRateHz:         8000,
			PulseExponent:        0.56,
			PulseScaleAdjustment: 1 / 54,
			PulseMaxAmplitude:    1,
			PulseMaxFrequencyHz:  40,
			PulseMinFrequencyHz:  23,
			PulseWidthMax:        0.5,
			PulseWidthMin:        0.1,
			MasterGain:           -15,
			ChassisVolume:        100,
			GearRaceVolume:       100,
			GearStreetVolume:     80,
		},
		Telemetry: Telemetry{
			Source: "udp://255.255.255.255:33739",
		},
	}

	viper.SetEnvPrefix("SIMTEZILO")
	viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))
	viper.AutomaticEnv()

	// viper.SetDefault("Synthesizer.sampleratehz", 8000)
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
		fmt.Printf("fatal error config file: %v\n", err)
	} else {
		err = viper.Unmarshal(c)
		if err != nil {
			fmt.Printf("fatal error unmarshalling config: %v\n", err)
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

	c.Synthesizer.PulseWidthMin = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMinFrequencyHz)
	c.Synthesizer.PulseWidthMax = float64(c.Synthesizer.SampleRateHz) / (2 * c.Synthesizer.PulseMaxFrequencyHz)

	return c
}
