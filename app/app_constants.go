package app

const frameRate = 60

const pitRadioFrameRate = frameRate / 2
const displayFrameRate = frameRate / 4
const engineHapticFrameRate = frameRate / 2
const hapticFrameRate = frameRate * 2
const telemetryFrameRate = frameRate

// TODO: find a better place for this
const snapMultiplier = 160

const (
	softReset = iota
	hardReset
)
