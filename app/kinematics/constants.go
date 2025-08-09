package kinematics

const GravityConstant = 9.80665 // m/s^2

// TODO: find a better place for gear constants
// Gear settings
const (
	NeutralGear int = 15
	ReverseGear int = 0
	NullGear    int = -100
)

var GearNames = map[int]string{
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
