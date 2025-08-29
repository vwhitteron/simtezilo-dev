package synth

type Buffer interface {
	Clear()
	Length() int
	Inspect(samples int) []float64
	Read(length int) []float64
	Write(samples []float64, overwrite bool)
}
