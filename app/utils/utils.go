package utils

// SumAngles90 sums two angles in degrees with the result being a valid 90-degree angle between 0 and 270 degrees.
//
// If a non-90 degree rotation is provided, the first angle is returned unmodified.
func SumAngle90(angle1 int, angle2 int) int {
	angle := angle1 + angle2

	if angle%90 != 0 {
		return angle1
	}

	return angle % 360
}
