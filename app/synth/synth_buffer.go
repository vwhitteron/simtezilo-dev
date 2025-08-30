package synth

type Buffer interface {
	Clear()
	Length() int
	Inspect(samples int, offset int) []float64
	Read(length int) []float64
	Write(samples []float64, offset int, overwrite bool)
	// WriteAtZeroCrossover(samples []float64)
}
