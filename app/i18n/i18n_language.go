package i18n

import (
	"strings"
	"time"

	"github.com/golang/freetype/truetype"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/en"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/jp"
)

const InvalidKey = "invalidkey"

type Font struct {
	Font  *truetype.Font
	Scale float64
}

type Language struct {
	Code        string
	Name        string
	String      map[string]string
	FontRegular Font
	FontValue   Font

	configLangCode *string

	fallback *Language
	log      zerolog.Logger
}

// NewLanguage creates a new Language instance based on the provided language code.
func NewLanguage(langCode *string, log zerolog.Logger) *Language {
	logger := log.With().Str("component", "i18n").Logger()

	language := getLanguage(*langCode, logger)
	language.configLangCode = langCode

	if language.Code != *langCode {
		logger.Warn().Str("requested", *langCode).Str("retrieved", language.Code).Msg("unsupported language")
	}

	go language.watchForConfigChanges()

	return language
}

// GetCurrentLanguage returns the currently configured language code.
func (l *Language) GetCurrentLanguage() string {
	return l.Code
}

// GetString retrieves the translation for the given key.
func (l *Language) GetString(key string) string {
	key = strings.ToLower(key)

	if val, ok := l.String[key]; ok {
		l.log.Debug().Str("key", key).Str("lang", l.Code).Str("result", "success").Msg("translation lookup")

		return val
	}

	l.log.Error().Str("key", key).Str("lang", l.Code).Str("result", "failure").Msg("translation lookup")

	return l.getFallbackString(key)
}

// GetFallbackString retrieves the fallback translation for the given key.
func (l *Language) getFallbackString(key string) string {
	key = strings.ToLower(key)

	if l.fallback == nil {
		l.log.Error().Str("key", key).Str("lang", "fallback").Str("result", "failure").Msg("translation lookup")

		return InvalidKey
	}

	if val, ok := l.fallback.String[key]; ok {
		l.log.Debug().Str("key", key).Str("lang", "fallback").Str("result", "success").Msg("translation lookup")

		return val
	}

	l.log.Error().Str("key", key).Str("lang", "fallback").Str("result", "failure").Msg("translation lookup")

	return InvalidKey
}

// TODO: is there a better way to integrate config changes?
func (l *Language) watchForConfigChanges() {
	l.log.Debug().Str("event", "start").Msg("config watch")

	for {
		time.Sleep(100 * time.Millisecond)

		if *l.configLangCode == l.Code {
			continue
		}

		language := getLanguage(*l.configLangCode, l.log)
		l.Code = language.Code
		l.Name = language.Name
		l.String = language.String
		l.FontRegular = language.FontRegular
		l.FontValue = language.FontValue
		l.fallback = language.fallback

		l.log.Debug().Str("language", l.Code).Str("event", "change").Msg("config watch")
	}
}

// getLanguage retrieves the language based on the provided country code.
func getLanguage(langCode string, logger zerolog.Logger) *Language {
	switch strings.ToLower(langCode) {
	case "jp":
		fontRegular, err := GetFont(jp.RegularFont)
		if err != nil {
			logger.Error().Err(err).Msg("failed to load regular font")
			fontRegular = nil
		}
		fontValue, err := GetFont(jp.ValueFont)
		if err != nil {
			logger.Error().Err(err).Msg("failed to load regular font")
			fontRegular = nil
		}
		return &Language{
			Code:   jp.Code,
			Name:   jp.Name,
			String: jp.Translations,
			FontRegular: Font{
				Font:  fontRegular,
				Scale: jp.RegularFontScale,
			},
			FontValue: Font{
				Font:  fontValue,
				Scale: jp.ValueFontScale,
			},
			fallback: getLanguage("en", logger),
			log:      logger,
		}
	default:
		fontEn, err := GetFont(en.RegularFont)
		if err != nil {
			logger.Error().Err(err).Msg("failed to load regular font")
			fontEn = nil
		}
		fontValue, err := GetFont(en.ValueFont)
		if err != nil {
			logger.Error().Err(err).Msg("failed to load value font")
			fontEn = nil
		}
		return &Language{
			Code:   en.Code,
			Name:   en.Name,
			String: en.Translations,
			FontRegular: Font{
				Font:  fontEn,
				Scale: en.RegularFontScale,
			},
			FontValue: Font{
				Font:  fontValue,
				Scale: en.ValueFontScale,
			},
			fallback: nil,
			log:      logger,
		}
	}
}
