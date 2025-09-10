package pitradio

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type DiscordBot struct {
	channelID string
	session   *discordgo.Session
}

// NewDiscordBot creates a new Discord bot instance.
// Requires a bot token and a channel ID.
func NewDiscordBot(token string, channelID string) (*DiscordBot, error) {
	if token == "" {
		return nil, fmt.Errorf("invalid token")
	}

	if channelID == "" {
		return nil, fmt.Errorf("invalid channel ID")
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return &DiscordBot{}, err
	}

	bot := DiscordBot{
		channelID: channelID,
		session:   dg,
	}

	return &bot, nil
}

// Connect establishes a connection to the Discord WebSocket.
func (d *DiscordBot) Connect() error {
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
		return nil
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
		return nil
	}

	_, err := d.session.ChannelMessageSend(d.channelID, message)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

// ready updates the watch status of the bot user
func ready(s *discordgo.Session, event *discordgo.Event) {
	err := s.UpdateWatchStatus(0, "Gran Turismo 7")
	if err != nil {
		fmt.Println(err.Error())
		return
	}
}
