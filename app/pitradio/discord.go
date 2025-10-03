package pitradio

import (
	"errors"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
)

type DiscordBot struct {
	channelID string             // Discord channel ID where messages will be sent
	session   *discordgo.Session // Currently connected Discord session
	queue     chan string        // Message queue for sending messages
}

// NewDiscordBot creates a new Discord bot instance.
// Requires a bot token and a channel ID.
func NewDiscordBot(token string, channelID string) (*DiscordBot, error) {
	if token == "" {
		return nil, errors.New("invalid token")
	}

	if channelID == "" {
		return nil, errors.New("invalid channel ID")
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return &DiscordBot{}, err
	}

	bot := DiscordBot{
		channelID: channelID,
		session:   dg,
		queue:     make(chan string, 100),
	}

	return &bot, nil
}

// Connect establishes a connection to the Discord WebSocket.
func (d *DiscordBot) Connect() error {
	if d.session == nil {
		return errors.New("discord session not initialized")
	}

	err := d.session.Open()
	if err != nil {
		return fmt.Errorf("open Discord session: %w", err)
	}

	d.session.AddHandler(ready)

	return nil
}

// Disconnect terminates the connection to the Discord WebSocket.
func (d *DiscordBot) Disconnect() error {
	if d.session == nil {
		return errors.New("discord session not initialized")
	}

	err := d.session.Close()
	if err != nil {
		return fmt.Errorf("close Discord session: %w", err)
	}

	return nil
}

// Send sends a message to the Discord channel.
func (d *DiscordBot) Send(message string) error {
	if d.session == nil {
		return errors.New("discord session not initialized")
	}

	d.queue <- message

	return nil
}

// MessageDispatcher processes the message queue and sends messages to Discord.
// This should be run as a goroutine.
func (d *DiscordBot) MessageDispatcher(logger zerolog.Logger) {
	for {
		select {
		case message := <-d.queue:
			_, err := d.session.ChannelMessageSend(d.channelID, message)
			if err != nil {
				logger.Error().
					Err(err).
					Str("package", "discord").
					Msg("send message to Discord channel")
			}
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// ready updates the watch status of the bot user.
func ready(s *discordgo.Session, event *discordgo.Event) {
	_ = s.UpdateWatchStatus(0, "Gran Turismo 7")
}
