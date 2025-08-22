//lint:file-ignore SA4026

package config

import appHaptics "github.com/vwhitteron/simtezilo-dev/app/haptics"

var defaultConfig = viperConfig{
	App: &app{
		Language: "en",
		LogLevel: "info",
	},
	Hardware: &hardware{
		Model:              "none",
		DisplayOrientation: 0,
	},
	Haptics: &haptics{
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
		_pulseWidthMax:               0.5,
		_pulseWidthMin:               0.1,
		EngineProfiles: map[string]appHaptics.EngineProfile{
			"s1":           {PrimaryBalance: 0.15, SecondaryBalance: 0.250, Gain: +0.0, PulseScale: 1.00},
			"i2":           {PrimaryBalance: 0.65, SecondaryBalance: 0.850, Gain: -2.0, PulseScale: 1.00},
			"i3":           {PrimaryBalance: 0.95, SecondaryBalance: 0.850, Gain: -2.0, PulseScale: 1.00},
			"i4":           {PrimaryBalance: 0.60, SecondaryBalance: 0.800, Gain: -3.5, PulseScale: 0.50},
			"i5":           {PrimaryBalance: 0.70, SecondaryBalance: 0.600, Gain: -1.0, PulseScale: 0.50},
			"i6":           {PrimaryBalance: 0.90, SecondaryBalance: 0.950, Gain: -2.5, PulseScale: 0.50},
			"i8":           {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -1.5, PulseScale: 0.50},
			"v4":           {PrimaryBalance: 0.65, SecondaryBalance: 0.850, Gain: -2.5, PulseScale: 1.00},
			"v6":           {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: +0.0, PulseScale: 0.50},
			"v6.b15.c120":  {PrimaryBalance: 0.88, SecondaryBalance: 0.940, Gain: +0.0, PulseScale: 0.50},
			"v6.b75.c120":  {PrimaryBalance: 0.83, SecondaryBalance: 0.940, Gain: +0.0, PulseScale: 0.50},
			"v6.b80.c120":  {PrimaryBalance: 0.82, SecondaryBalance: 0.930, Gain: +0.0, PulseScale: 0.50},
			"v6.b90.c120":  {PrimaryBalance: 0.80, SecondaryBalance: 0.920, Gain: +0.0, PulseScale: 0.50},
			"v6.b120.c120": {PrimaryBalance: 0.75, SecondaryBalance: 0.880, Gain: +0.0, PulseScale: 0.50},
			"v8":           {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -3.0, PulseScale: 0.25},
			"v8.b90.c180":  {PrimaryBalance: 0.85, SecondaryBalance: 0.920, Gain: +0.0, PulseScale: 0.50},
			"v10":          {PrimaryBalance: 0.88, SecondaryBalance: 0.965, Gain: -2.0, PulseScale: 0.25},
			"v10.b90.v72":  {PrimaryBalance: 0.86, SecondaryBalance: 0.940, Gain: -1.5, PulseScale: 0.25},
			"v12":          {PrimaryBalance: 0.99, SecondaryBalance: 0.995, Gain: -2.0, PulseScale: 0.25},
			"v12.b60.c120": {PrimaryBalance: 0.96, SecondaryBalance: 0.985, Gain: -2.0, PulseScale: 0.25},
			"v12.b75.c60":  {PrimaryBalance: 0.97, SecondaryBalance: 0.988, Gain: -2.0, PulseScale: 0.25},
			"v12.b100.c60": {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -1.5, PulseScale: 0.25},
			"v12.b144.c60": {PrimaryBalance: 0.93, SecondaryBalance: 0.975, Gain: -1.5, PulseScale: 0.25},
			"w16":          {PrimaryBalance: 0.99, SecondaryBalance: 0.995, Gain: -1.5, PulseScale: 0.25},
			"h2":           {PrimaryBalance: 0.70, SecondaryBalance: 0.950, Gain: +0.0, PulseScale: 1.00},
			"h4":           {PrimaryBalance: 0.80, SecondaryBalance: 0.950, Gain: +0.0, PulseScale: 1.00},
			"h6":           {PrimaryBalance: 0.92, SecondaryBalance: 0.975, Gain: -1.5, PulseScale: 0.50},
			"h12":          {PrimaryBalance: 0.98, SecondaryBalance: 0.990, Gain: +0.0, PulseScale: 0.25},
			"K2":           {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: +0.0, PulseScale: 0.33},
			"K4":           {PrimaryBalance: 0.75, SecondaryBalance: 0.880, Gain: +0.0, PulseScale: 0.16},
		},
	},
	Synthesizer: &Synthesizer{
		SampleRateHz:              8000,
		OutputFile:                "",
		MasterGain:                -15,
		GainIncrement:             0.25,
		ChassisGain:               0,
		TransmissionGain:          -2,
		TransmissionGainMinRace:   -7,
		TransmissionGainMinStreet: -10,
		EngineGain:                -8,
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
