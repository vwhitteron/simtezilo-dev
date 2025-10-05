package pitradio

import (
	"github.com/rs/zerolog"
)

// Message represents a text message with an associated language code.
type Message struct {
	Text    string // The text message to be converted to speech
	Lang    string // Speech language code (IETF BCP-47)
	Accent  string // Optional regional accent locale code
	NoCache bool   // Disable caching of the result when true (typically for highly unique messages)
}

// PitRadio defines the interface for a service that can send text and voice messages.
type PitRadio interface {
	Connect() error                          // Connect to the pit radio service
	Disconnect() error                       // Disconnect from the pit radio service
	MessageDispatcher(logger zerolog.Logger) // Process the voice message queue and send messages
	Send(message Message) error              // Enqueue a message to the dispatcher
}
