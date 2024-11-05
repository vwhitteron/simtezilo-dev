package internal

import (
	"strconv"
)

func gearName(gearNum int) string {
	gearName, ok := gearNames[gearNum]
	if !ok {
		gearName = strconv.Itoa(gearNum)
	}

	return gearName
}
