// Package pitradio provides an interface for sending text and voice messages as a pit radio analog.
package pitradio

import (
	"sync"

	"github.com/rs/zerolog"
)

// MessageType defines the type of message to be sent.
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

type LogOutput struct {
	logger zerolog.Logger
}

// NewLogOutput creates a new PitRadio implementation that outputs messages to the logging system.
func NewLogOutput(logger *zerolog.Logger) (*LogOutput, error) {
	return &LogOutput{
		logger: logger.With().Str("component", "pitradioLogOutput").Logger(),
	}, nil
}

// BackgroundTask runs any necessary background tasks for the log output.
func (p *LogOutput) BackgroundTask() {
	// No background task needed for log output
}

// Close cleans up resources used by the log output.
func (p *LogOutput) Close() error {
	// No resources to clean up for log output
	return nil
}

// Send logs the message to the logging system.
func (p *LogOutput) Send(message Message) error {
	messageType := "text"
	if message.MessageType == AudioMessage {
		messageType = "audio"
	}

	p.logger.Info().
		Str("type", messageType).
		Str("text", message.Text).
		Msg("Pit radio message sent")

	return nil
}

// MultiOutput fans a single stream of pit-radio messages out to several
// PitRadio implementations (for example Discord and a local audio device),
// so the same notifications can be delivered through multiple channels.
type MultiOutput struct {
	outputs []PitRadio
	log     zerolog.Logger
}

// NewMultiOutput creates a MultiOutput delivering to each of the given outputs.
func NewMultiOutput(logger zerolog.Logger, outputs ...PitRadio) *MultiOutput {
	return &MultiOutput{
		outputs: outputs,
		log:     logger.With().Str("component", "pitradioMulti").Logger(),
	}
}

// BackgroundTask runs every output's background task concurrently and blocks
// until they all return.
func (m *MultiOutput) BackgroundTask() {
	var waitGroup sync.WaitGroup

	for _, output := range m.outputs {
		waitGroup.Add(1)

		go func(o PitRadio) {
			defer waitGroup.Done()

			o.BackgroundTask()
		}(output)
	}

	waitGroup.Wait()
}

// Send delivers the message to every output, returning the first error.
func (m *MultiOutput) Send(message Message) error {
	var firstErr error

	for _, output := range m.outputs {
		err := output.Send(message)
		if err != nil {
			m.log.Warn().Err(err).Msg("deliver pit-radio message")

			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// Close closes every output, returning the first error.
func (m *MultiOutput) Close() error {
	var firstErr error

	for _, output := range m.outputs {
		err := output.Close()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}
