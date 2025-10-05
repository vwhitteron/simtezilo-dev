package pitradio

import (
	"bytes"
	_ "embed"
	"encoding/binary"

	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/cache"
)

const (
	discordSampleRate   = 48000 // Discord requires 48kHz sample rate
	discordChannels     = 2     // Discord requires stereo audio
	discordFrameSize    = 960   // 20ms frame size at 48kHz
	discordMaxFrameSize = 3840  // Maximum bytes per frame (for Opus)
)

// DiscordOptions holds the configuration for creating a new Discord bot.
type DiscordOptions struct {
	Token          string       // Discord bot token
	ChannelID      string       // Discord channel ID for text messages
	VoiceChannelID string       // Discord voice channel ID for audio
	GuildID        string       // Discord guild ID
	Cache          *cache.Cache // Cache manager for storing processed TTS audio
}

type DiscordBot struct {
	channelID      string                     // Discord channel ID where messages will be sent
	voiceChannelID string                     // Discord voice channel ID for audio
	guildID        string                     // Discord guild ID for voice connections
	cache          *cache.Cache               // Cache manager for storing processed TTS audio
	session        *discordgo.Session         // Currently connected Discord session
	voiceConn      *discordgo.VoiceConnection // Voice connection for audio
	queue          chan Message               // Message queue for sending messages
}

// NewDiscordBot creates a new Discord bot instance.
func NewDiscordBot(config DiscordOptions) (*DiscordBot, error) {
	if config.Token == "" {
		return nil, errors.New("invalid token")
	}

	if config.ChannelID+config.ChannelID+config.VoiceChannelID == "" {
		return nil, errors.New("no guild or channel IDs provided")
	}

	if config.VoiceChannelID != "" && config.GuildID == "" {
		return nil, errors.New("guild ID is required when voice channel ID is provided")
	}

	session, err := discordgo.New("Bot " + config.Token)
	if err != nil {
		return &DiscordBot{}, err
	}

	// session.LogLevel = discordgo.LogDebug

	bot := DiscordBot{
		channelID:      config.ChannelID,
		voiceChannelID: config.VoiceChannelID,
		guildID:        config.GuildID,
		cache:          config.Cache,
		session:        session,
		queue:          make(chan Message, 100),
	}

	return &bot, nil
}

// Connect establishes a connection to the Discord WebSocket and joins the voice channel.
func (d *DiscordBot) Connect() error {
	if d.session == nil {
		return errors.New("discord session not initialized")
	}

	err := d.session.Open()
	if err != nil {
		return fmt.Errorf("open Discord session: %w", err)
	}

	d.session.AddHandler(ready)

	// Automatically join voice channel
	err = d.joinVoiceChannel()
	if err != nil {
		return fmt.Errorf("join voice channel: %w", err)
	}

	return nil
}

// Disconnect terminates the connection to the Discord WebSocket.
func (d *DiscordBot) Disconnect() error {
	if d.session == nil {
		return errors.New("discord session not initialized")
	}

	// Leave voice channel if connected
	if d.voiceConn != nil {
		_ = d.leaveVoiceChannel()
	}

	err := d.session.Close()
	if err != nil {
		return fmt.Errorf("close Discord session: %w", err)
	}

	return nil
}

// Send sends a message to the Discord channel.
func (d *DiscordBot) Send(message Message) error {
	if d.session == nil {
		return errors.New("discord session not initialized")
	}

	d.queue <- message

	return nil
}

// MessageDispatcher processes the message queue and sends voice messages to Discord.
// This should be run as a goroutine.
func (d *DiscordBot) MessageDispatcher(logger zerolog.Logger) {
	for {
		select {
		case message := <-d.queue:
			if d.channelID != "" {
				_, err := d.session.ChannelMessageSend(d.channelID, message.Text)
				if err != nil {
					logger.Error().
						Err(err).
						Str("package", "discord").
						Msg("send message to Discord channel")
				}
			}

			err := d.voiceMessageSend(message)
			if err != nil {
				logger.Error().
					Err(err).
					Str("package", "discord").
					Msg("send voice message to Discord channel")
			}
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// joinVoiceChannel joins the configured voice channel for audio streaming.
func (d *DiscordBot) joinVoiceChannel() error {
	if d.session == nil {
		return errors.New("discord session not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	vc, err := d.session.ChannelVoiceJoin(ctx, d.guildID, d.voiceChannelID, false, false)
	if err != nil {
		return fmt.Errorf("join voice channel: %w", err)
	}

	d.voiceConn = vc

	// Wait for voice connection to be ready
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for voice connection to be ready: %w", ctx.Err())
		default:
			if d.voiceConn.Status == discordgo.VoiceConnectionStatusReady {
				return nil
			}

			time.Sleep(100 * time.Millisecond)
		}
	}
}

// leaveVoiceChannel leaves the current voice channel.
func (d *DiscordBot) leaveVoiceChannel() error {
	if d.voiceConn == nil {
		return nil
	}

	err := d.voiceConn.Disconnect(context.Background())
	if err != nil {
		return fmt.Errorf("leave voice channel: %w", err)
	}

	d.voiceConn = nil

	return nil
}

func (d *DiscordBot) voiceMessageSend(message Message) error {
	if d.voiceConn == nil {
		return errors.New("not connected to voice channel")
	}

	if d.voiceConn.Status != discordgo.VoiceConnectionStatusReady {
		return errors.New("voice connection not ready")
	}

	// Generate cache filename
	cacheID := fmt.Sprintf("%s_%s_%s", message.Lang, message.Accent, message.Text)

	// Try to read from cache first
	dcaData, err := d.cache.Read(cacheID)
	if err == nil && len(dcaData) > 0 {
		return d.sendVoiceAudio(dcaData)
	}

	// Cache miss - generate TTS audio data using TextToSpeech
	mp3Data, err := TextToSpeech(message)
	if err != nil {
		return fmt.Errorf("generate TTS audio: %w", err)
	}

	// Convert MP3 data to PCM
	pcmData, err := mpegtoPCM(mp3Data)
	if err != nil {
		return fmt.Errorf("convert MP3 to PCM: %w", err)
	}

	// Convert PCM data to DCA format
	dcaData = encodeToDCA(pcmData)
	if len(dcaData) == 0 {
		return errors.New("failed to encode audio to DCA format")
	}

	// Cache the DCA data for future use
	err = d.cache.Write(cacheID, dcaData)
	if err != nil {
		// Log error but don't fail the operation
		fmt.Printf("Warning: failed to cache DCA data: %v\n", err)
	}

	// Send DCA to voice channel
	return d.sendVoiceAudio(dcaData)
}

// sendVoiceAudio plays a DCA audio buffer to the voice channel.
func (d *DiscordBot) sendVoiceAudio(dca []byte) error {
	if d.voiceConn == nil {
		return errors.New("not connected to voice channel")
	}

	if d.voiceConn.Status != discordgo.VoiceConnectionStatusReady {
		return errors.New("voice connection not ready")
	}

	// Send speaking packet
	err := d.voiceConn.Speaking(true)
	if err != nil {
		return fmt.Errorf("start speaking: %w", err)
	}

	defer func() {
		// Always stop speaking when done
		_ = d.voiceConn.Speaking(false)
	}()

	var opusLen int16

	reader := bytes.NewReader(dca)

	for {
		// Read opus frame length from dca file.
		err = binary.Read(reader, binary.LittleEndian, &opusLen)

		// If this is the end of the file, just return.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}

		if err != nil {
			fmt.Println("Error reading from dca file :", err)

			return err
		}

		// Read encoded pcm from dca file.
		buf := make([]byte, opusLen)
		err = binary.Read(reader, binary.LittleEndian, &buf)

		// Should not be any end of file errors
		if err != nil {
			fmt.Println("Error reading from dca file :", err)

			return err
		}

		d.voiceConn.OpusSend <- buf
	}
}

// ready updates the watch status of the bot user.
func ready(s *discordgo.Session, event *discordgo.Event) {
	_ = s.UpdateWatchStatus(0, "Gran Turismo 7")
}
