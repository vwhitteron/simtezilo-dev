package haptics

type EngineProfile struct {
	PrimaryBalance   float64
	SecondaryBalance float64
}

// EngineProfiles defines the haptic profiles for different engine types
//
// Primary Balance: 0.0 (unbalanced) to 1.0 (perfectly balanced)
// Secondary Balance: 0.0 (unbalanced) to 1.0 (perfectly balanced)
//
// Engine Layouts:
// D: Radial (reserved)
// H: Horizontally opposed
// I: Inline/straight
// K: Wankel rotary (kreiskolbenmotor/KKM)
// R: Rotary (reserved)
// S: 2 stroke
// T: Turbine (reserved)
// V: V
// W: W
var EngineProfiles = map[string]EngineProfile{
	"S1": { // 1 cylinder 2 stroke
		PrimaryBalance:   0.15,
		SecondaryBalance: 0.25,
	},
	"I3": { // 3 cylinder 0° bank, 120° crank plane
		PrimaryBalance:   0.95,
		SecondaryBalance: 0.85,
	},
	"I4": { // 4 cylinder 0° bank, 180° crank plane
		PrimaryBalance:   0.6,
		SecondaryBalance: 0.8,
	},
	"I5": { // 5 cylinder 0° bank, 72° crank plane
		PrimaryBalance:   0.7,
		SecondaryBalance: 0.6,
	},
	"I6": { // 6 cylinder 0° bank, 120° crank plane
		PrimaryBalance:   0.9,
		SecondaryBalance: 0.95,
	},
	"I8": { // 8 cylinder 0° bank, 90° crank plane
		PrimaryBalance:   0.95,
		SecondaryBalance: 0.98,
	},
	"V4": { // 4 cylinder 90° bank, 90° crank plane
		PrimaryBalance:   0.65,
		SecondaryBalance: 0.85,
	},
	"V6": { // 6 cylinder 60° bank, 120° crank plane
		PrimaryBalance:   0.85,
		SecondaryBalance: 0.96,
	},
	"V6.B15.C120": { // 6 cylinder 15° bank, 120° crank plane (VR6)
		PrimaryBalance:   0.88,
		SecondaryBalance: 0.94,
	},
	"V6.B75.C120": { // 6 cylinder 75° bank, 120° crank plane
		PrimaryBalance:   0.83,
		SecondaryBalance: 0.94,
	},
	"V6.B80.C120": { // 6 cylinder 80° bank, 120° crank plane
		PrimaryBalance:   0.82,
		SecondaryBalance: 0.93,
	},
	"V6.B90.C120": { // 6 cylinder 90° bank, 120° crank plane
		PrimaryBalance:   0.80,
		SecondaryBalance: 0.92,
	},
	"V6.B120.C120": { // 6 cylinder 120° bank, 120° crank plane
		PrimaryBalance:   0.75,
		SecondaryBalance: 0.88,
	},
	"V8": { // 8 cylinder 90° bank, 90° crank plane
		PrimaryBalance:   0.95,
		SecondaryBalance: 0.98,
	},
	"V8.B90.C180": { // 8 cylinder 90° bank, 180° crank plane
		PrimaryBalance:   0.85,
		SecondaryBalance: 0.92,
	},
	"V10": { // 10 cylinder 72° bank, 72° crank plane
		PrimaryBalance:   0.88,
		SecondaryBalance: 0.965,
	},
	"V10.B90.V72": { // 10 cylinder 90° bank, 72° crank plane
		PrimaryBalance:   0.86,
		SecondaryBalance: 0.94,
	},
	"V12": { // 12 cylinder 60° bank, 60° crank plane
		PrimaryBalance:   0.99,
		SecondaryBalance: 0.995,
	},
	"V12.B60.C120": { // 12 cylinder 60° bank, 120° crank plane
		PrimaryBalance:   0.96,
		SecondaryBalance: 0.985,
	},
	"V12.B75.C60": { // 12 cylinder 75° bank, 60° crank plane
		PrimaryBalance:   0.97,
		SecondaryBalance: 0.988,
	},
	"V12.B100.C60": { // 12 cylinder 100° bank, 60° crank plane
		PrimaryBalance:   0.95,
		SecondaryBalance: 0.98,
	},
	"V12.B144.C60": { // 12 cylinder 144° bank, 60° crank plane
		PrimaryBalance:   0.93,
		SecondaryBalance: 0.975,
	},
	"W16": { // 16 cylinder W, 90° bank, 45° crank plane (dual VR8)
		PrimaryBalance:   0.99,
		SecondaryBalance: 0.995,
	},
	"H4": { // 4 cylinder 180° bank, 180° crank plane (boxer)
		PrimaryBalance:   0.8,
		SecondaryBalance: 0.95,
	},
	"H6": { // 6 cylinder 180° bank, 120° crank plane
		PrimaryBalance:   0.92,
		SecondaryBalance: 0.975,
	},
	"H12": { // 12 cylinder 180° bank, 120° crank plane
		PrimaryBalance:   0.98,
		SecondaryBalance: 0.99,
	},
	"K2": { // 2 rotor Wankel rotary
		PrimaryBalance:   0.85,
		SecondaryBalance: 0.96,
	},
	"K4": { // 4 rotor Wankel rotary
		PrimaryBalance:   0.75,
		SecondaryBalance: 0.88,
	},
}
