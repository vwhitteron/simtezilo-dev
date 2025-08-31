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
			"s1":                  {PrimaryBalance: 0.15, SecondaryBalance: 0.250, Gain: +0.00, PulseScale: 1.00}, // Racing Kart 125 Shifter
			"i2":                  {PrimaryBalance: 0.65, SecondaryBalance: 0.850, Gain: -2.00, PulseScale: 1.00}, // 500 F '68
			"i3":                  {PrimaryBalance: 0.95, SecondaryBalance: 0.850, Gain: -5.00, PulseScale: 1.00}, // COPEN RJ VGT
			"i4":                  {PrimaryBalance: 0.72, SecondaryBalance: 0.800, Gain: -4.50, PulseScale: 0.75}, // Supra GT500 '97
			"i4_rmed":             {PrimaryBalance: 0.76, SecondaryBalance: 0.800, Gain: -3.25, PulseScale: 0.68}, // NSX CONCEPT-GT '16
			"i5":                  {PrimaryBalance: 0.60, SecondaryBalance: 0.700, Gain: -2.50, PulseScale: 0.60}, // Sport quattro S1 Pikes Peak '87
			"i6":                  {PrimaryBalance: 0.90, SecondaryBalance: 0.950, Gain: -3.75, PulseScale: 0.68}, // GR Supra Racing Concept '18
			"i8":                  {PrimaryBalance: 0.92, SecondaryBalance: 0.980, Gain: -3.00, PulseScale: 0.32}, // W 196 R '55
			"v4":                  {PrimaryBalance: 0.70, SecondaryBalance: 0.850, Gain: -1.50, PulseScale: 0.90}, // 919 Hybrid '16
			"v6":                  {PrimaryBalance: 0.80, SecondaryBalance: 0.950, Gain: -2.50, PulseScale: 0.50}, // REF v6_b60_c120_rstd
			"v6_b15_c120_rstd":    {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: -2.75, PulseScale: 0.50}, // Volkswagen GTI VGT (Gr.3)
			"v6_b60_c120_rstd":    {PrimaryBalance: 0.80, SecondaryBalance: 0.950, Gain: -2.50, PulseScale: 0.50}, // R.S.01 GT3 '16"
			"v6_b75_c120_rstd":    {PrimaryBalance: 0.80, SecondaryBalance: 0.720, Gain: -1.50, PulseScale: 0.60}, // NSX Gr.3
			"v6_b80_c120_rhigh":   {PrimaryBalance: 0.06, SecondaryBalance: 0.930, Gain: -3.00, PulseScale: 0.44}, // MP4/4 '88
			"v6_b90_c120_rstd":    {PrimaryBalance: 0.72, SecondaryBalance: 0.720, Gain: +0.00, PulseScale: 0.60}, // GR010 HYBRID '21
			"v6_b90_c120_rhigh":   {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: +0.00, PulseScale: 0.31}, // Red Bull X2019 Competition
			"v6_b120_c120_rstd":   {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: -3.50, PulseScale: 0.70}, // R18 TDI '11
			"v8":                  {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -2.75, PulseScale: 0.40}, // REF v8_b90_c90_rstd
			"v8_b90_c180_rstd":    {PrimaryBalance: 0.72, SecondaryBalance: 0.980, Gain: -1.00, PulseScale: 0.44}, // R92CP '92
			"v8_b90_c180_rmed":    {PrimaryBalance: 0.86, SecondaryBalance: 0.980, Gain: -3.00, PulseScale: 0.30}, // TS030 Hybrid '12
			"v8_b90_c90_rstd":     {PrimaryBalance: 0.95, SecondaryBalance: 0.980, Gain: -2.75, PulseScale: 0.40}, // M6 GT3 Endurance Model '16
			"v10":                 {PrimaryBalance: 0.86, SecondaryBalance: 0.940, Gain: -3.00, PulseScale: 0.40}, // REF v10_b90_c72_rstd
			"v10_b72_c72_rmed":    {PrimaryBalance: 0.86, SecondaryBalance: 0.950, Gain: -1.50, PulseScale: 0.25}, // Lexus LFA '10
			"v10_b90_c72_rstd":    {PrimaryBalance: 0.86, SecondaryBalance: 0.940, Gain: -3.00, PulseScale: 0.40}, // Huracán GT3 '15
			"v12":                 {PrimaryBalance: 0.90, SecondaryBalance: 0.990, Gain: +0.00, PulseScale: 0.30}, // REF v12_b60_c120_rstd
			"v12_b60_c120_rstd":   {PrimaryBalance: 0.90, SecondaryBalance: 0.990, Gain: +0.00, PulseScale: 0.30}, // McLaren F1 GTR - BMW '95
			"v12_b60_c120_rhigh":  {PrimaryBalance: 0.80, SecondaryBalance: 0.960, Gain: -1.50, PulseScale: 0.33}, // 1500T-A
			"v12_b75_c120_rstd":   {PrimaryBalance: 0.80, SecondaryBalance: 0.990, Gain: -1.00, PulseScale: 0.30}, // V12 Vantage GT3 '12
			"v12_b100_c120_rstd":  {PrimaryBalance: 0.77, SecondaryBalance: 0.980, Gain: -1.50, PulseScale: 0.33}, // 908 HDi FAP '10
			"v12_b144_c120_rhigh": {PrimaryBalance: 0.80, SecondaryBalance: 0.970, Gain: -1.50, PulseScale: 0.12}, // SRT Tomahawk X VGT
			"w16":                 {PrimaryBalance: 0.80, SecondaryBalance: 0.990, Gain: -3.50, PulseScale: 0.20}, // Veyron Gr.4
			"h2":                  {PrimaryBalance: 0.70, SecondaryBalance: 0.950, Gain: +0.00, PulseScale: 1.00}, // Toyota Sports 800 '65
			"h4":                  {PrimaryBalance: 0.80, SecondaryBalance: 0.950, Gain: -3.75, PulseScale: 0.80}, // BRZ GT300 '21
			"h6":                  {PrimaryBalance: 0.92, SecondaryBalance: 0.970, Gain: -2.00, PulseScale: 0.50}, // 911 RSR (991) '17
			"h12":                 {PrimaryBalance: 0.98, SecondaryBalance: 0.990, Gain: +0.00, PulseScale: 0.25}, // 917K '70
			"k2":                  {PrimaryBalance: 0.85, SecondaryBalance: 0.960, Gain: +0.00, PulseScale: 0.33}, // RE Amemiya FD3S RX-7
			"k4":                  {PrimaryBalance: 0.75, SecondaryBalance: 0.800, Gain: +0.00, PulseScale: 0.10}, // 787B '91
		},
	},
	Synthesizer: &Synthesizer{
		InternalSampleRateHz:      8000,
		OutputSampleRateHz:        8000,
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
