package clipboard

import (
	"net/url"
	"strings"

	"github.com/atotto/clipboard"
)

var clipboardReadAll = clipboard.ReadAll

type Validator struct {
	allowedSchemes map[string]bool
}

func NewValidator() *Validator {
	return &Validator{
		allowedSchemes: map[string]bool{"http": true, "https": true},
	}
}

func (v *Validator) ExtractURL(text string) string {
	text = strings.TrimSpace(text)

	// Quick reject: too long, contains newlines, or obviously not a URL
	if len(text) > 2048 || strings.ContainsAny(text, "\n\r") {
		return ""
	}

	// Must start with http:// or https://
	if !strings.HasPrefix(text, "http://") && !strings.HasPrefix(text, "https://") {
		return ""
	}

	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" || !v.allowedSchemes[parsed.Scheme] {
		return ""
	}

	return parsed.String()
}

func ReadURL() string {
	text, err := clipboardReadAll()
	if err != nil {
		return ""
	}
	validator := NewValidator()
	return validator.ExtractURL(text)
}
