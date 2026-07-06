package calibrator

// Calibrator defines the methods required for calibration operations.
type Calibrator interface {
	IsEnabled() bool
	IsStopping() bool
	GetFrequency() float64
	GetGain() float64
	GetChannel() OutputChannel
	GetTargetChannel() int
	GetSweepFrequency() float64
	ConfirmStopped()
}
