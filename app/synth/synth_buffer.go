package synth

type Buffer interface {
	Clear()
	Length() int
	Read(length int) []float64
	Write(samples []float64, overwrite bool)
	Advance(samples int)
}
