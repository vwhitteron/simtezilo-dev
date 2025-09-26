package vector

import (
	"math"

	"github.com/zetetos/gt-telemetry/pkg/models"
)

func Delta(vector1 models.Vector, vector2 models.Vector) models.Vector {
	return models.Vector{
		X: vector1.X - vector2.X,
		Y: vector1.Y - vector2.Y,
		Z: vector1.Z - vector2.Z,
	}
}

func Magnitude(vector models.Vector) float64 {
	return math.Sqrt(float64(vector.X*vector.X + vector.Y*vector.Y + vector.Z*vector.Z))
}

func Scale(vector models.Vector, xScale float32, yScale float32, zScale float32) models.Vector {
	return models.Vector{
		X: vector.X * xScale,
		Y: vector.Y * yScale,
		Z: vector.Z * zScale,
	}
}
