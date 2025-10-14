package tyres

import (
	"github.com/zetetos/gt-telemetry/pkg/models"
)

// attribute holds the temperature and condition of a single tyre.
type attribute struct {
	temperature float32
	condition   Condition
}

// Tyre provides temperature and condition information for all four tyres.
type Tyre struct {
	optimalLower  float32
	optimalUpper  float32
	coldThreshold float32
	hotThreshold  float32
	frontLeft     attribute
	frontRight    attribute
	rearLeft      attribute
	rearRight     attribute
}

// ConditionMap maps tyre conditions to the list of tyre positions in that condition.
type ConditionMap map[Condition][]Position

// Position represents the physical location of a tyre on the vehicle.
type Position int

const (
	PositionFront Position = iota
	PositionRear
	PositionFrontLeft
	PositionFrontRight
	PositionRearLeft
	PositionRearRight
)

// Condition represents the operating condition of a tyre.
type Condition string

const (
	ConditionInvalid Condition = "invalid"
	ConditionOptimal Condition = "optimal"
	ConditionHot     Condition = "hot"
	ConditionCold    Condition = "cold"
)

// String returns the string representation of the Condition.
func (c Condition) String() string {
	return string(c)
}

// String returns the string representation of the Position.
func (p *Position) String() string {
	switch *p {
	case PositionFront:
		return "front"
	case PositionRear:
		return "rear"
	case PositionFrontLeft:
		return "front left"
	case PositionFrontRight:
		return "front right"
	case PositionRearLeft:
		return "rear left"
	case PositionRearRight:
		return "rear right"
	default:
		return "unknown"
	}
}

// New creates a tyreAttributes struct from the given tyre temperatures.
func New(optimalCenter float32, optimalWindow float32, margin float32, tyreTemps models.CornerSet) Tyre {
	optimalMin := optimalCenter - (optimalWindow / 2)
	optimalMax := optimalCenter + (optimalWindow / 2)

	attributes := Tyre{
		optimalLower:  optimalMin,
		optimalUpper:  optimalMax,
		coldThreshold: optimalMin - margin,
		hotThreshold:  optimalMax + margin,
		frontLeft: attribute{
			temperature: tyreTemps.FrontLeft,
		},
		frontRight: attribute{
			temperature: tyreTemps.FrontRight,
		},
		rearLeft: attribute{
			temperature: tyreTemps.RearLeft,
		},
		rearRight: attribute{
			temperature: tyreTemps.RearRight,
		},
	}

	attributes.assessTyreConditions()

	return attributes
}

// Condition returns the temperature of the tyre at the given position.
func (a *Tyre) Condition(position Position) string {
	switch position {
	case PositionFront:
		return ConditionInvalid.String()
	case PositionRear:
		return ConditionInvalid.String()
	case PositionFrontLeft:
		return a.frontLeft.condition.String()
	case PositionFrontRight:
		return a.frontRight.condition.String()
	case PositionRearLeft:
		return a.rearLeft.condition.String()
	case PositionRearRight:
		return a.rearRight.condition.String()
	default:
		return ConditionInvalid.String()
	}
}

// Temperature returns the temperature of the tyre at the given position.
func (a *Tyre) Temperature(position Position) float32 {
	switch position {
	case PositionFront:
		return (a.frontLeft.temperature + a.frontRight.temperature) / 2
	case PositionRear:
		return (a.rearLeft.temperature + a.rearRight.temperature) / 2
	case PositionFrontLeft:
		return a.frontLeft.temperature
	case PositionFrontRight:
		return a.frontRight.temperature
	case PositionRearLeft:
		return a.rearLeft.temperature
	case PositionRearRight:
		return a.rearRight.temperature
	default:
		return 0
	}
}

// averageTemperature calculates the average temperature of all four tyres.
func (a *Tyre) AverageTemperature() float32 {
	return (a.frontLeft.temperature + a.frontRight.temperature + a.rearLeft.temperature + a.rearRight.temperature) / 4.0
}

// getTyreConditions returns a map of conditions to lists of tyres in that condition.
func (a *Tyre) Conditions() ConditionMap {
	conditionMap := make(ConditionMap)

	// Map each tyre position to its condition
	tyreStates := map[Position]Condition{
		PositionFrontLeft:  a.frontLeft.condition,
		PositionFrontRight: a.frontRight.condition,
		PositionRearLeft:   a.rearLeft.condition,
		PositionRearRight:  a.rearRight.condition,
	}

	// Group tyres by their condition
	for position, condition := range tyreStates {
		conditionMap[condition] = append(conditionMap[condition], position)
	}

	return conditionMap
}

// containsCondition checks if any tyre has the given condition.
func (a *Tyre) ContainsCondition(condition Condition) bool {
	return a.frontLeft.condition == condition ||
		a.frontRight.condition == condition ||
		a.rearLeft.condition == condition ||
		a.rearRight.condition == condition
}

// allTyresOptimal determines if all tyres are in optimal.
// It returns true if the following conditions are met:
// 1. Average temperature of all 4 corners is within the ideal range.
// 2. None of the individual tyres are overheating.
func (a *Tyre) ConditionOptimal() bool {
	// Not optimal if any tyre is hot
	if a.ContainsCondition(ConditionHot) {
		return false
	}

	averageTemp := a.AverageTemperature()

	if averageTemp < a.optimalLower {
		return false
	}

	if averageTemp > a.optimalUpper {
		return false
	}

	return true
}

// equal compares the state of this tyreTempState with another to see if they differ.
func (a *Tyre) Equal(comparison Tyre) bool {
	return comparison.frontLeft == a.frontLeft &&
		comparison.frontRight == a.frontRight &&
		comparison.rearLeft == a.rearLeft &&
		comparison.rearRight == a.rearRight
}

// assessTyreConditions evaluates and sets the condition for each tyre based on its temperature.
func (a *Tyre) assessTyreConditions() {
	corners := []*attribute{&a.frontLeft, &a.frontRight, &a.rearLeft, &a.rearRight}

	for _, corner := range corners {
		temp := corner.temperature

		if temp <= 0 {
			corner.condition = ConditionInvalid

			continue
		}

		switch {
		case temp < a.coldThreshold:
			corner.condition = ConditionCold
		case temp > a.hotThreshold:
			corner.condition = ConditionHot
		case temp >= a.optimalLower && temp <= a.optimalUpper:
			corner.condition = ConditionOptimal
		default:
			// In warming-up range, treat as optimal even though not strictly optimal/cold/hot
			corner.condition = ConditionOptimal
		}
	}
}
