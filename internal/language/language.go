// Package language centralizes supported target languages and Piper voices.
package language

import (
	"fmt"
	"strings"
)

// Language describes a translation target and its default Piper voice.
type Language struct {
	DisplayName     string
	Code            string
	TranslationName string
	Voice           string
}

var supported = []Language{
	{
		DisplayName:     "Portuguese (Brazil)",
		Code:            "pt-BR",
		TranslationName: "Brazilian Portuguese (pt-BR)",
		Voice:           "pt_BR-faber-medium",
	},
	{
		DisplayName:     "Spanish",
		Code:            "es",
		TranslationName: "Spanish (es)",
		Voice:           "es_ES-davefx-medium",
	},
	{
		DisplayName:     "French",
		Code:            "fr",
		TranslationName: "French (fr)",
		Voice:           "fr_FR-siwis-medium",
	},
	{
		DisplayName:     "German",
		Code:            "de",
		TranslationName: "German (de)",
		Voice:           "de_DE-thorsten-medium",
	},
	{
		DisplayName:     "Italian",
		Code:            "it",
		TranslationName: "Italian (it)",
		Voice:           "it_IT-riccardo-x_low",
	},
}

// Supported returns a copy of the supported language list.
func Supported() []Language {
	out := make([]Language, len(supported))
	copy(out, supported)
	return out
}

// ByCode resolves common language-code aliases.
func ByCode(code string) (Language, error) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "_", "-"))
	for _, lang := range supported {
		candidate := strings.ToLower(strings.ReplaceAll(lang.Code, "_", "-"))
		if normalized == candidate {
			return lang, nil
		}
	}

	// Preserve aliases accepted by the Python reference implementation.
	aliases := map[string]string{
		"pt-br": "pt-BR",
		"es-es": "es",
		"fr-fr": "fr",
		"de-de": "de",
		"it-it": "it",
	}
	if canonical, ok := aliases[normalized]; ok {
		return ByCode(canonical)
	}
	return Language{}, fmt.Errorf("unsupported language %q; supported: pt-BR, es, fr, de, it", code)
}

// ByDisplayName resolves a GUI label.
func ByDisplayName(display string) (Language, error) {
	for _, lang := range supported {
		if lang.DisplayName == display {
			return lang, nil
		}
	}
	return Language{}, fmt.Errorf("unsupported language %q", display)
}
