package pitradio

import "github.com/rs/zerolog"

type PitRadioService interface {
	Connect() error                          // Connect to the pit radio service
	Disconnect() error                       // Disconnect from the pit radio service
	MessageDispatcher(logger zerolog.Logger) // Process the message queue and send messages
	Send(message string) error               // Enqueue a message to the dispatcher
}
