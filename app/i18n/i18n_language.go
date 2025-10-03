package i18n

import (
	"strings"
	"time"

	"github.com/golang/freetype/truetype"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/en"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/jp"
	translationkeys "github.com/vwhitteron/simtezilo-dev/app/i18n/translations"
)

const InvalidKey = "invalidkey"

type Font struct {
	Font  *truetype.Font
	Scale float64
}

type Language struct {
	Code        string
	Name        string
	Keys        map[translationkeys.Key]string
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
func (l *Language) GetString(key translationkeys.Key) string {
	key = key.ToLower()

	if val, ok := l.Keys[key]; ok {
		l.log.Debug().Str("key", key.String()).Str("lang", l.Code).Str("result", "success").Msg("translation lookup")

		return val
	}

	l.log.Error().Str("key", key.String()).Str("lang", l.Code).Str("result", "failure").Msg("translation lookup")

	return l.getFallbackString(key)
}

// GetFallbackString retrieves the fallback translation for the given key.
func (l *Language) getFallbackString(key translationkeys.Key) string {
	key = key.ToLower()

	if l.fallback == nil {
		l.log.Error().Str("key", key.String()).Str("lang", "fallback").Str("result", "failure").Msg("translation lookup")

		return InvalidKey
	}

	if val, ok := l.fallback.Keys[key]; ok {
		l.log.Debug().Str("key", key.String()).Str("lang", "fallback").Str("result", "success").Msg("translation lookup")

		return val
	}

	l.log.Error().Str("key", key.String()).Str("lang", "fallback").Str("result", "failure").Msg("translation lookup")

	return InvalidKey
}

// TODO: is there a better way to integrate config changes?
func (l *Language) watchForConfigChanges() {
	l.log.Debug().Str("event", "start").Msg("config watch")

	for {
		// TODO: this delay causes the setting title to lag behind changes to the language
		time.Sleep(200 * time.Millisecond)

		if *l.configLangCode == l.Code {
			continue
		}

		language := getLanguage(*l.configLangCode, l.log)
		l.Code = language.Code
		l.Name = language.Name
		l.Keys = language.Keys
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
			Code: jp.Code,
			Name: jp.Name,
			Keys: jp.Translations,
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
			Code: en.Code,
			Name: en.Name,
			Keys: en.Translations,
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
