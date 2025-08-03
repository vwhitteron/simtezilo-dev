package i18n

import (
	"strings"

	"github.com/golang/freetype/truetype"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/en"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/jajp"
)

const InvalidKey = "invalidkey"

var languageCodes = []string{
	"en",
	"ja-jp",
}

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

	fallback *Language
	log      zerolog.Logger
}

func NewLanguage(lang string, log zerolog.Logger) *Language {
	log = log.With().Str("component", "i18n").Str("lang", lang).Logger()

	switch lang {
	case "en":
		lang := GetLanguage("en")
		lang.log = log

		return lang
	case "ja-jp":
		lang := GetLanguage("ja-jp")
		lang.log = log

		return lang
	default: // fall back to English
		log.Warn().Str("lang", lang).Msg("unsupported language, falling back to English")

		lang := GetLanguage("en")
		lang.log = log

		return lang
	}

}

func GetLanguage(lang string) *Language {
	switch strings.ToLower(lang) {
	case "ja-jp":
		fontRegular, err := GetFont(jajp.RegularFont)
		if err != nil {
			log.Error().Err(err).Msg("failed to load regular font")
			fontRegular = nil
		}
		fontValue, err := GetFont(jajp.ValueFont)
		if err != nil {
			log.Error().Err(err).Msg("failed to load regular font")
			fontRegular = nil
		}
		return &Language{
			Code:   jajp.Code,
			Name:   jajp.Name,
			String: jajp.Translations,
			FontRegular: Font{
				Font:  fontRegular,
				Scale: jajp.RegularFontScale,
			},
			FontValue: Font{
				Font:  fontValue,
				Scale: jajp.ValueFontScale,
			},
			fallback: GetLanguage("en"),
		}
	default:
		fontEn, err := GetFont(en.RegularFont)
		if err != nil {
			log.Error().Err(err).Msg("failed to load regular font")
			fontEn = nil
		}
		fontValue, err := GetFont(en.ValueFont)
		if err != nil {
			log.Error().Err(err).Msg("failed to load value font")
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
		}
	}
}

func (l *Language) GetString(key string) string {
	key = strings.ToLower(key)

	if val, ok := l.String[key]; ok {
		l.log.Debug().Str("key", key).Str("lang", l.Code).Str("result", "success").Msg("translation lookup")

		return val
	}

	l.log.Error().Str("key", key).Str("lang", l.Code).Str("result", "failure").Msg("translation lookup")

	return l.getFallbackString(key)
}

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

func (l *Language) GetCurrentLanguage() string {
	return l.Code
}

// TODO: need to update a.display.device.Font to use the new font
func (l *Language) NextLanguage() string {
	for i, lang := range languageCodes {
		if lang == l.Code {
			nextIndex := (i + 1) % len(languageCodes)

			nextLang := GetLanguage(languageCodes[nextIndex])
			l.Code = nextLang.Code
			l.Name = nextLang.Name
			l.String = nextLang.String
			l.FontRegular = nextLang.FontRegular
			l.FontValue = nextLang.FontValue
			l.fallback = nextLang.fallback

			break
		}
	}

	return l.Code
}

// TODO: need to update a.display.device.Font to use the new font
func (l *Language) PreviousLanguage() string {
	for i, lang := range languageCodes {
		if lang == l.Code {
			prevIndex := (i - 1 + len(languageCodes)) % len(languageCodes)

			prevLang := GetLanguage(languageCodes[prevIndex])
			l.Code = prevLang.Code
			l.Name = prevLang.Name
			l.String = prevLang.String
			l.FontRegular = prevLang.FontRegular
			l.FontValue = prevLang.FontValue
			l.fallback = prevLang.fallback

			break
		}
	}

	return l.Code
}
