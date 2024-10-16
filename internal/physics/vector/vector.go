package vector

import (
	"math"

	telemetry_client "github.com/vwhitteron/gt-telemetry"
)

func Delta(vector1 telemetry_client.Vector, vector2 telemetry_client.Vector) telemetry_client.Vector {
	return telemetry_client.Vector{
		X: vector1.X - vector2.X,
		Y: vector1.Y - vector2.Y,
		Z: vector1.Z - vector2.Z,
	}
}

func Scale(vector telemetry_client.Vector, xScale float32, yScale float32, zScale float32) telemetry_client.Vector {
	return telemetry_client.Vector{
		X: vector.X * xScale,
		Y: vector.Y * yScale,
		Z: vector.Z * zScale,
	}
}

func Magnitude(vector telemetry_client.Vector) float64 {
	return math.Sqrt(float64(vector.X*vector.X + vector.Y*vector.Y + vector.Z*vector.Z))
}
