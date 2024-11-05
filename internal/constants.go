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

// Display settings
const gearFontSize = 48
const volumeFontSize = 24
