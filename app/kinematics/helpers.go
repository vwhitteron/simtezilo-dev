package kinematics

import (
	"strconv"

	"github.com/vwhitteron/simtezilo-dev/app/kinematics/vector"
	"github.com/vwhitteron/simtezilo-dev/app/signal"
)

// no haptics when vehicle comes to a controlled stop
// TODO: check angular velocity, etc to enable for uncontrolled stops
// if vector.Magnitude(c.kinematics.Current.Velocity.Vector) >= 0.28 {
func (k *KinematicsTracker) VehicleIsInMotion() bool {
	lastMag := vector.Magnitude(k.Last.SixDOFTranslationCalc.Velocity)
	currentMag := vector.Magnitude(k.Current.SixDOFTranslationCalc.Velocity)
	if signal.LargestMagnitude(lastMag, currentMag) >= 0.28 {
		return true
	}

	return false
}

func GearName(gearNum int) string {
	gearName, ok := GearNames[gearNum]
	if !ok {
		gearName = strconv.Itoa(gearNum)
	}

	return gearName
}
