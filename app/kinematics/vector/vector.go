package vector

import (
	"math"

	gtmodels "github.com/zetetos/gt-telemetry/pkg/models"
)

func Delta(vector1 gtmodels.Vector, vector2 gtmodels.Vector) gtmodels.Vector {
	return gtmodels.Vector{
		X: vector1.X - vector2.X,
		Y: vector1.Y - vector2.Y,
		Z: vector1.Z - vector2.Z,
	}
}

func Magnitude(vector gtmodels.Vector) float64 {
	return math.Sqrt(float64(vector.X*vector.X + vector.Y*vector.Y + vector.Z*vector.Z))
}

func Scale(vector gtmodels.Vector, xScale float32, yScale float32, zScale float32) gtmodels.Vector {
	return gtmodels.Vector{
		X: vector.X * xScale,
		Y: vector.Y * yScale,
		Z: vector.Z * zScale,
	}
}
