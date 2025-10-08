package pitradio

type MessageType int

const (
	TextMessage  MessageType = iota // Plain text message to be converted to speech
	AudioMessage                    // Pre-recorded audio message to be played as-is
)

// Message represents a text message with an associated language code.
type Message struct {
	MessageType MessageType // Type of message (text or audio)
	Text        string      // The text message to be converted to speech
	Lang        string      // Speech language code (IETF BCP-47)
	Accent      string      // Optional regional accent locale code
	Audio       []byte      // Pre-generated audio data
	NoCache     bool        // Disable caching of the result when true (typically for highly unique messages)
}

// PitRadio defines the interface for a service that can send text and voice messages.
type PitRadio interface {
	BackgroundTask()            // Management task to run in the background
	Close() error               // Disconnect from the pit radio service
	Send(message Message) error // Enqueue a message to the dispatcher
}
