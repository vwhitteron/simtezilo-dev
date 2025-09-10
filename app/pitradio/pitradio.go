package pitradio

type PitRadioService interface {
	Connect() error
	Disconnect() error
	Send(message string) error
}
