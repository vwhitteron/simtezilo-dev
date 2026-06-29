package synthesizer

// Buffer defines the interface for audio buffers used in the synthesizer.
type Buffer interface {
	Clear()
	Length() int
	Inspect(samples int, offset int) []float64
	Read(dst []float64) int
	Write(samples []float64, offset int, overwrite bool)
	WriteScaled(samples []float64, magnitude float64, offset int, overwrite bool)
	Used() int
}
