package discord //nolint:testpackage // white-box: inspects the unexported queue

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vwhitteron/simtezilo-dev/app/pitradio"
)

func testSendNeverBlocks(t *testing.T) {
	t.Parallel()

	// Arrange
	bot, err := New(Config{
		Enabled:        true,
		Token:          "token",
		GuildID:        "guild",
		VoiceChannelID: "voice",
		MessageGap:     time.Second,
		Logger:         zerolog.Nop(),
	})
	require.NoError(t, err)

	done := make(chan struct{})

	// Act
	go func() {
		defer close(done)

		for range 1000 {
			_ = bot.Send(pitradio.Message{Text: "hello"})
		}
	}()

	// Assert
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail(t, "Send blocked")
	}

	assert.LessOrEqual(t, bot.queue.Len(), pitradio.QueueCapacity)
}

func testSendDisabled(t *testing.T) {
	t.Parallel()

	// Arrange
	bot, err := New(Config{
		Enabled: false,
		Logger:  zerolog.Nop(),
	})
	require.NoError(t, err)

	// Act
	err = bot.Send(pitradio.Message{Text: "hello"})

	// Assert
	require.NoError(t, err)
	assert.Equal(t, 0, bot.queue.Len())
}

func testSendDuringClose(t *testing.T) {
	t.Parallel()

	// Arrange
	bot, err := New(Config{
		Enabled:        true,
		Token:          "token",
		GuildID:        "guild",
		VoiceChannelID: "voice",
		MessageGap:     time.Second,
		Logger:         zerolog.Nop(),
	})
	require.NoError(t, err)

	done := make(chan struct{})

	// Act
	go func() {
		defer close(done)

		for range 100 {
			_ = bot.Send(pitradio.Message{Text: "hello"})
		}
	}()

	_ = bot.Close()

	<-done

	// Assert
	assert.False(t, bot.isEnabled())
}

func TestSend(t *testing.T) {
	t.Parallel()

	t.Run("testSendNeverBlocks", testSendNeverBlocks)
	t.Run("testSendDisabled", testSendDisabled)
	t.Run("testSendDuringClose", testSendDuringClose)
}
