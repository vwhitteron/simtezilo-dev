package codec

// Opus/Discord audio constants. These are plain values (no CGO) shared by callers
// that request effect samples at the Discord rate and by the codec/dca subpackage
// that performs the actual Opus encoding. The Opus encoder itself lives in
// codec/dca so that importing codec does not pull in the CGO gopus dependency.
const (
	OpusSampleRate   = 48000 // Discord requires 48kHz sample rate
	OpusChannels     = 2     // Discord requires stereo audio
	OpusFrameSize    = 960   // 20ms frame size at 48kHz
	OpusMaxFrameSize = 3840  // Maximum bytes per Opus frame
)
