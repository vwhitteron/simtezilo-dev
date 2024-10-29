package internal

const frameRate = 60
const gravityConstant = 9.81
const vehicleMaxSpeedKPH = 700
const vehicleTeleportDisplacement = (vehicleMaxSpeedKPH / 3.6) / frameRate
const pitLaneSpeedLimit = 60 / 3.6

// Gear settings
const (
	NeutralGear int = 15
	ReverseGear int = 0
	NullGear    int = -100
)

var gearNames = map[int]string{
	-100: "NULL",
	0:    "R",
	1:    "1",
	2:    "2",
	3:    "3",
	4:    "4",
	5:    "5",
	6:    "6",
	7:    "7",
	8:    "8",
	9:    "9",
	10:   "10",
	15:   "N",
}

// Synthesizer settings
// const maxGain = 0
const synthSampleRateHz = 8000

// Display settings
const gearFontSize = 48
const volumeFontSize = 24

// Haptics settings
const pulseExponent = float64(0.56)
const pulseScaleAdjustment = float64(1 / 54.0)
const pulseMaxAmplitude = float64(1.0)
const pulseMaxFrequencyHz = float64(40)
const pulseMinFrequencyHz = float64(23)
const pulseWidthMax = synthSampleRateHz / (2 * pulseMinFrequencyHz)
const pulseWidthMin = synthSampleRateHz / (2 * pulseMaxFrequencyHz)
