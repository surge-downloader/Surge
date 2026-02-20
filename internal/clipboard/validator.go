package clipboard

import (
	"strings"

	"github.com/atotto/clipboard"
	"github.com/surge-downloader/surge/internal/source"
)

var clipboardReadAll = clipboard.ReadAll

type Validator struct {
}

func NewValidator() *Validator {
	return &Validator{}
}

func (v *Validator) ExtractURL(text string) string {
	text = strings.TrimSpace(text)

	// Quick reject: too long, contains newlines, or obviously not a URL
	if len(text) > 2048 || strings.ContainsAny(text, "\n\r") {
		return ""
	}

	if !source.IsSupported(text) {
		return ""
	}

	return source.Normalize(text)
}

func ReadURL() string {
	text, err := clipboardReadAll()
	if err != nil {
		return ""
	}
	validator := NewValidator()
	return validator.ExtractURL(text)
}
