package languagedb

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/golang/freetype/truetype"
	"github.com/rs/zerolog"
	"github.com/vwhitteron/simtezilo-dev/app/i18n/font"
)

// languageMetadata holds the metadata for a language.
type languageMetadata struct {
	Code string `json:"Code"`
	Name string `json:"Name"`
}

// languageFont holds the font file name and size for a language.
type languageFont struct {
	File string  `json:"File"`
	Size float64 `json:"Size"`
}

// languageFonts holds a list of font variations and sizes for a language.
type languageFonts map[string]languageFont

// languageTranslations contains a list of key-value pairs for translations for a language.
type languageTranslations map[string]string

// languageData holds the data and attributes for a language.
type languageData struct {
	Metadata     languageMetadata     `json:"Metadata"`
	Fonts        languageFonts        `json:"Fonts"`
	Translations languageTranslations `json:"Translations"`
}

// LanguageDB manages the loading and retrieval of translations for multiple languages.
type LanguageDB struct {
	db  map[string]languageData
	log zerolog.Logger
}

// New creates a new LanguageDB instance and loads the embedded language files.
func New(log zerolog.Logger) (*LanguageDB, error) {
	translation := &LanguageDB{
		db:  make(map[string]languageData),
		log: log.With().Str("component", "i18n").Logger(),
	}

	err := translation.loadDBFromEmbeddedFS()
	if err != nil {
		return nil, fmt.Errorf("load languages: %w", err)
	}

	return translation, nil
}

// NewFromJSON creates a new LanguageDB instance and loads language data from the provided JSON byte slice.
func NewFromJSON(jsonData []byte, log zerolog.Logger) (*LanguageDB, error) {
	translation := &LanguageDB{
		db:  make(map[string]languageData),
		log: log.With().Str("component", "i18n").Logger(),
	}

	err := translation.loadDBFromJSON(jsonData)
	if err != nil {
		return nil, fmt.Errorf("load languages: %w", err)
	}

	return translation, nil
}

// LanguageCodes returns a slice of all available language codes.
func (l *LanguageDB) LanguageCodes() []string {
	languageCodes := []string{}

	for _, lang := range l.db {
		languageCodes = append(languageCodes, lang.Metadata.Code)
	}

	return languageCodes
}

// ValidateCode checks if the provided language code is valid.
func (l *LanguageDB) ValidateCode(code string) bool {
	validCodes := l.LanguageCodes()

	for _, validCode := range validCodes {
		if code == validCode {
			return true
		}
	}

	return false
}

// String retrieves the translation for the given language code and key.
// It falls back to English if the translation is not found in the requested language.
// If the translation cannot be found even in English it returns an empty string.
func (l *LanguageDB) String(code string, key Key) (value string) {
	key = key.ToLower()

	if language, ok := l.db[code]; ok {
		if value, ok := language.Translations[key.String()]; ok {
			return value
		}
	} else {
		l.log.Warn().
			Str("key", key.String()).
			Str("lang code", code).
			Str("result", "failure").
			Msg("invalid language code")
	}

	l.log.Warn().
		Str("key", key.String()).
		Str("lang code", code).
		Str("result", "failure").
		Msg("translation lookup")

	// Fallback to English
	if code != "en" {
		value = l.String("en", key)
	}

	return value
}

// RegularFont returns the font used for displaying regular text for the given language code.
func (l *LanguageDB) RegularFont(code string) font.Font {
	return l.getFont("regular", code)
}

// ItalicFont returns the font used for displaying italic text for the given language code.
func (l *LanguageDB) ItalicFont(code string) font.Font {
	return l.getFont("italic", code)
}

// ValueFont returns the font used for displaying values for the given language code.
func (l *LanguageDB) ValueFont(code string) font.Font {
	return l.getFont("value", code)
}

// getFont returns the font for the given language code.
func (l *LanguageDB) getFont(variation string, code string) font.Font {
	var truetypeFont *truetype.Font

	// Attempt to load the requested font
	if language, ok := l.db[code]; ok {
		var err error

		fontName := ""

		switch strings.ToLower(variation) {
		case "regular":
			fontName = language.Fonts["Regular"].File
		case "italic":
			fontName = language.Fonts["Italic"].File
		case "value":
			fontName = language.Fonts["Value"].File
		default:
			fontName = language.Fonts["Regular"].File

			l.log.Warn().
				Str("variation", variation).
				Msg("unknown font variation, defaulting to regular")
		}

		truetypeFont, err = font.GetFont(fontName)
		if err != nil {
			l.log.Error().
				Err(err).
				Str("lang code", code).
				Str("font", fontName).
				Msg("load regular font")
		}
	}

	// Fallback to English regular if the font couldn't be loaded
	if truetypeFont == nil && (code != "en" || variation != "regular") {
		truetypeFont = l.getFont("regular", "en").Font
	}

	return font.Font{
		Font:  truetypeFont,
		Scale: 1.0,
	}
}

//go:embed lang/*
var languageFiles embed.FS

// loadDBFromEmbeddedFS loads all language files from the embedded filesystem.
func (l *LanguageDB) loadDBFromEmbeddedFS() error {
	if len(l.db) > 0 {
		return errors.New("language database already loaded")
	}

	entries, err := languageFiles.ReadDir("lang")
	if err != nil {
		return fmt.Errorf("read embedded language directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			l.log.Warn().
				Str("file", entry.Name()).
				Msg("skipping non-JSON file in language directory")

			continue
		}

		data, err := languageFiles.ReadFile("lang/" + entry.Name())
		if err != nil {
			l.log.Error().
				Err(err).
				Str("file", entry.Name()).
				Msg("read embedded language file")

			continue
		}

		var langData languageData

		err = json.Unmarshal(data, &langData)
		if err != nil {
			l.log.Error().
				Err(err).
				Str("file", entry.Name()).
				Msg("unmarshal language JSON")

			continue
		}

		if langData.Metadata.Code != "" {
			l.db[langData.Metadata.Code] = langData
		}
	}

	return nil
}

// loadDBFromJSON loads language data from a JSON byte slice.
func (l *LanguageDB) loadDBFromJSON(data []byte) error {
	if len(l.db) > 0 {
		return errors.New("language database already loaded")
	}

	var langDB []languageData

	err := json.Unmarshal(data, &langDB)
	if err != nil {
		l.log.Error().
			Err(err).
			Msg("unmarshal language JSON")
	}

	for _, langData := range langDB {
		if langData.Metadata.Code != "" {
			l.db[langData.Metadata.Code] = langData
		}
	}

	return nil
}
