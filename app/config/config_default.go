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
			"s1":           {PrimaryBalance: 0.15, SecondaryBalance: 0.250, Gain: +0.00, PulseScale: 1.00},
			"i2":           {PrimaryBalance: 0.65, SecondaryBalance: 0.850, Gain: -2.00, PulseScale: 1.00},
			"i3":           {PrimaryBalance: 0.95, SecondaryBalance: 0.850, Gain: -5.00, PulseScale: 1.00}, // Daihatsu COPEN RJ VGT
			"i4":           {PrimaryBalance: 0.76, SecondaryBalance: 0.800, Gain: -3.25, PulseScale: 0.68}, // Toyota Supra GT500 '97
			"i5":           {PrimaryBalance: 0.60, SecondaryBalance: 0.600, Gain: -2.50, PulseScale: 0.60}, // Audi Sport quattro S1 Pikes Peak '87
			"i6":           {PrimaryBalance: 0.90, SecondaryBalance: 0.950, Gain: -2.00, PulseScale: 0.50}, // Toyota GR Supra Racing Concept '18
			"i8":           {PrimaryBalance: 0.92, SecondaryBalance: 0.980, Gain: -3.00, PulseScale: 0.32}, // Mercedes-Benz W 196 R '55
			"v4":           {PrimaryBalance: 0.70, SecondaryBalance: 0.850, Gain: -1.50, PulseScale: 0.90}, // Porsche 919 Hybrid '16
			"v6":           {PrimaryBalance: 0.80, SecondaryBalance: 0.960, Gain: -2.50, PulseScale: 0.50}, // REF v6_b60_c120
			"v6_b15_c120":  {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: -2.75, PulseScale: 0.50}, // Volkswagen GTI VGT (Gr.3)
			"v6_b60_c120":  {PrimaryBalance: 0.80, SecondaryBalance: 0.960, Gain: -2.50, PulseScale: 0.50}, // Renault R.S.01 GT3 '16
			"v6_b75_c120":  {PrimaryBalance: 0.83, SecondaryBalance: 0.940, Gain: +0.00, PulseScale: 0.50}, // Honda NSX Gr.3
			"v6_b80_c120":  {PrimaryBalance: 0.62, SecondaryBalance: 0.930, Gain: -3.00, PulseScale: 0.33}, // Mclaren MP4/4 '88
			"v6_b90_c120":  {PrimaryBalance: 0.80, SecondaryBalance: 0.720, Gain: +0.00, PulseScale: 0.60}, // Toyota GR010 HYBRID '21
			"v6_b120_c120": {PrimaryBalance: 0.75, SecondaryBalance: 0.880, Gain: +0.00, PulseScale: 0.50}, // Audo R18 TDI '11
			"v8":           {PrimaryBalance: 0.86, SecondaryBalance: 0.980, Gain: -4.00, PulseScale: 0.55}, // REF v8_b90_c90
			"v8_b90_c180":  {PrimaryBalance: 0.78, SecondaryBalance: 0.980, Gain: -5.00, PulseScale: 0.33}, // Nissan R92CP '92
			"v8_b90_c90":   {PrimaryBalance: 0.86, SecondaryBalance: 0.980, Gain: -4.00, PulseScale: 0.55}, // M6 GT3 Endurance Model '16
			"v10":          {PrimaryBalance: 0.88, SecondaryBalance: 0.965, Gain: -2.00, PulseScale: 0.25}, // REF v10_b72_c72
			"v10_b72_v72":  {PrimaryBalance: 0.86, SecondaryBalance: 0.940, Gain: -1.50, PulseScale: 0.25},
			"v10_b90_v72":  {PrimaryBalance: 0.86, SecondaryBalance: 0.940, Gain: -1.50, PulseScale: 0.25},
			"v12":          {PrimaryBalance: 0.99, SecondaryBalance: 0.995, Gain: -2.00, PulseScale: 0.25}, // REF v12_b60_c120
			"v12_b60_c120": {PrimaryBalance: 0.96, SecondaryBalance: 0.985, Gain: -2.00, PulseScale: 0.25},
			"v12_b75_c60":  {PrimaryBalance: 0.97, SecondaryBalance: 0.988, Gain: -2.00, PulseScale: 0.25},
			"v12_b100_c60": {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -1.50, PulseScale: 0.25},
			"v12_b144_c60": {PrimaryBalance: 0.93, SecondaryBalance: 0.975, Gain: -1.50, PulseScale: 0.25},
			"w16":          {PrimaryBalance: 0.99, SecondaryBalance: 0.995, Gain: -1.50, PulseScale: 0.25},
			"h2":           {PrimaryBalance: 0.70, SecondaryBalance: 0.950, Gain: +0.00, PulseScale: 1.00},
			"h4":           {PrimaryBalance: 0.80, SecondaryBalance: 0.950, Gain: -3.75, PulseScale: 0.80}, // BRZ GT300 '21
			"h6":           {PrimaryBalance: 0.92, SecondaryBalance: 0.975, Gain: -2.00, PulseScale: 0.50}, // Porsche 911 RSR (991) '17
			"h12":          {PrimaryBalance: 0.98, SecondaryBalance: 0.990, Gain: +0.00, PulseScale: 0.25},
			"k2":           {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: +0.00, PulseScale: 0.33},
			"k4":           {PrimaryBalance: 0.75, SecondaryBalance: 0.800, Gain: +0.00, PulseScale: 0.10}, // Mazda 787B '91
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
