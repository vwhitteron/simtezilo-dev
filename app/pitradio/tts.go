package pitradio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// textToSpeech converts a given text message into speech audio data using Google Translate TTS.
// It returns the audio data in MP3 format within an MPEG container.
func textToSpeech(message Message) ([]byte, error) {
	if message.Text == "" {
		return nil, errors.New("text cannot be empty")
	}

	if message.Lang == "" {
		message.Lang = "en"
	}

	locale := message.Lang

	if message.Accent != "" {
		locale = message.Lang + "-" + strings.ToUpper(message.Accent)
	}

	textLen := len(message.Text)
	safeText := url.QueryEscape(message.Text)
	url := fmt.Sprintf(
		"https://translate.google.com/translate_tts?ie=UTF-8&tl=%s&textlen=%d&client=tw-ob&q=%s",
		locale,
		textLen,
		safeText,
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create TTS request: %w", err)
	}

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make TTS request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TTS request failed with status: %d", resp.StatusCode)
	}

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read audio data: %w", err)
	}

	return audioData, nil
}
