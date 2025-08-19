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
		EngineFrequencyMin:           15,
		EngineFrequencyMax:           45,
		EngineProfiles: map[string]appHaptics.EngineProfile{
			"S1":           {PrimaryBalance: 0.15, SecondaryBalance: 0.250, Gain: 0.0, PulseScale: 1.00},
			"I3":           {PrimaryBalance: 0.95, SecondaryBalance: 0.850, Gain: -2.0, PulseScale: 1.00},
			"I4":           {PrimaryBalance: 0.60, SecondaryBalance: 0.800, Gain: -3.5, PulseScale: 0.50},
			"I5":           {PrimaryBalance: 0.70, SecondaryBalance: 0.600, Gain: -1.0, PulseScale: 0.50},
			"I6":           {PrimaryBalance: 0.90, SecondaryBalance: 0.950, Gain: -2.5, PulseScale: 0.50},
			"I8":           {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -1.5, PulseScale: 0.50},
			"V4":           {PrimaryBalance: 0.65, SecondaryBalance: 0.850, Gain: -2.5, PulseScale: 1.00},
			"V6":           {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: 0.0, PulseScale: 0.50},
			"V6.B15.C120":  {PrimaryBalance: 0.88, SecondaryBalance: 0.940, Gain: 0.0, PulseScale: 0.50},
			"V6.B75.C120":  {PrimaryBalance: 0.83, SecondaryBalance: 0.940, Gain: 0.0, PulseScale: 0.50},
			"V6.B80.C120":  {PrimaryBalance: 0.82, SecondaryBalance: 0.930, Gain: 0.0, PulseScale: 0.50},
			"V6.B90.C120":  {PrimaryBalance: 0.80, SecondaryBalance: 0.920, Gain: 0.0, PulseScale: 0.50},
			"V6.B120.C120": {PrimaryBalance: 0.75, SecondaryBalance: 0.880, Gain: 0.0, PulseScale: 0.50},
			"V8":           {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -3.0, PulseScale: 0.25},
			"V8.B90.C180":  {PrimaryBalance: 0.85, SecondaryBalance: 0.920, Gain: 0.0, PulseScale: 0.50},
			"V10":          {PrimaryBalance: 0.88, SecondaryBalance: 0.965, Gain: -2.0, PulseScale: 0.25},
			"V10.B90.V72":  {PrimaryBalance: 0.86, SecondaryBalance: 0.940, Gain: -1.5, PulseScale: 0.25},
			"V12":          {PrimaryBalance: 0.99, SecondaryBalance: 0.995, Gain: -2.0, PulseScale: 0.25},
			"V12.B60.C120": {PrimaryBalance: 0.96, SecondaryBalance: 0.985, Gain: -2.0, PulseScale: 0.25},
			"V12.B75.C60":  {PrimaryBalance: 0.97, SecondaryBalance: 0.988, Gain: -2.0, PulseScale: 0.25},
			"V12.B100.C60": {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -1.5, PulseScale: 0.25},
			"V12.B144.C60": {PrimaryBalance: 0.93, SecondaryBalance: 0.975, Gain: -1.5, PulseScale: 0.25},
			"W16":          {PrimaryBalance: 0.99, SecondaryBalance: 0.995, Gain: -1.5, PulseScale: 0.25},
			"H4":           {PrimaryBalance: 0.80, SecondaryBalance: 0.950, Gain: 0.0, PulseScale: 1.00},
			"H6":           {PrimaryBalance: 0.92, SecondaryBalance: 0.975, Gain: -1.5, PulseScale: 0.50},
			"H12":          {PrimaryBalance: 0.98, SecondaryBalance: 0.990, Gain: 0.0, PulseScale: 0.25},
			"K2":           {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: 0.0, PulseScale: 0.33},
			"K4":           {PrimaryBalance: 0.75, SecondaryBalance: 0.880, Gain: 0.0, PulseScale: 0.16},
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
