package pitradio

type PitRadioService interface {
	Connect() error            // Connect to the pit radio service
	Disconnect() error         // Disconnect from the pit radio service
	Send(message string) error // Send a message to the pit radio service
}
