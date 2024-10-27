package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type App struct {
	AssetDir string
	LogLevel string
}

type Audio struct {
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
}

type Hardware struct {
	Model              string
	DisplayOrientation int
}
type Display struct {
	GearFontSize   int
	VolumeFontSize int
}

type Config struct {
	App      App
	Audio    Audio
	Display  Display
	Hardware Hardware
}

func NewConfig(filename string) *Config {
	c := &Config{
		App: App{
			AssetDir: "assets",
			LogLevel: "info",
		},
		Audio: Audio{
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
		Display: Display{
			GearFontSize:   48,
			VolumeFontSize: 20,
		},
		Hardware: Hardware{
			Model:              "none",
			DisplayOrientation: 0,
		},
	}

	viper.SetEnvPrefix("RACESIG")
	viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))
	viper.AutomaticEnv()

	// viper.SetDefault("audio.sampleratehz", 8000)
	// viper.SetDefault("Audio.PulseExponent", 0.56)
	// viper.SetDefault("Audio.pulseScaleAdjustment", 1/54)
	// viper.SetDefault("Audio.pulseMaxAmplitude", 1)
	// viper.SetDefault("Audio.pulseMaxFrequencyHz", 40)
	// viper.SetDefault("Audio.pulseMinFrequencyHz", 23)
	// viper.SetDefault("Audio.pulseWidthMax", 0.5)
	// viper.SetDefault("Audio.pulseWidthMin", 0.1)
	// viper.SetDefault("Display.gearFontSize", 16)
	// viper.SetDefault("Display.volumeFontSize", 16)

	viper.SetConfigName(filename)
	viper.SetConfigType("toml")
	viper.AddConfigPath("/boot/racesig/")
	viper.AddConfigPath("/opt/racesig/")
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

	fmt.Printf("Config: %+v\n", c)

	c.Audio.PulseWidthMin = float64(c.Audio.SampleRateHz) / (2 * c.Audio.PulseMinFrequencyHz)
	c.Audio.PulseWidthMax = float64(c.Audio.SampleRateHz) / (2 * c.Audio.PulseMaxFrequencyHz)

	return c
}
