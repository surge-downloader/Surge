package clipboard

import (
	"errors"
	"strings"
	"testing"
)

func TestNewValidator(t *testing.T) {
	v := NewValidator()
	if v == nil {
		t.Fatal("NewValidator() returned nil")
	}
	if v.allowedSchemes == nil {
		t.Fatal("NewValidator() did not initialize allowedSchemes")
	}
	if !v.allowedSchemes["http"] {
		t.Error("NewValidator() did not allow http")
	}
	if !v.allowedSchemes["https"] {
		t.Error("NewValidator() did not allow https")
	}
}

func TestValidator_ExtractURL(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Valid cases
		{
			name:     "Simple HTTP",
			input:    "http://example.com",
			expected: "http://example.com",
		},
		{
			name:     "Simple HTTPS",
			input:    "https://example.com",
			expected: "https://example.com",
		},
		{
			name:     "URL with path",
			input:    "https://example.com/path/to/resource",
			expected: "https://example.com/path/to/resource",
		},
		{
			name:     "URL with query params",
			input:    "https://example.com?q=search&lang=en",
			expected: "https://example.com?q=search&lang=en",
		},
		{
			name:     "URL with fragment",
			input:    "https://example.com#section",
			expected: "https://example.com#section",
		},
		{
			name:     "URL with port",
			input:    "http://localhost:8080",
			expected: "http://localhost:8080",
		},

		// Trimming
		{
			name:     "Leading/Trailing spaces",
			input:    "  https://example.com  ",
			expected: "https://example.com",
		},

		// Invalid cases - formatting/content
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Just whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "Contains newlines in middle",
			input:    "https://exa\nmple.com",
			expected: "",
		},
		{
			name:     "Trailing newline",
			input:    "https://example.com\n",
			expected: "https://example.com",
		},
		{
			name:     "Contains carriage return",
			input:    "https://exa\rmple.com",
			expected: "",
		},
		{
			name:     "No scheme",
			input:    "example.com",
			expected: "",
		},
		{
			name:     "Just scheme",
			input:    "http://",
			expected: "",
		},
		{
			name:     "Just scheme s",
			input:    "https://",
			expected: "",
		},

		// Invalid cases - Schemes
		{
			name:     "FTP scheme",
			input:    "ftp://example.com",
			expected: "",
		},
		{
			name:     "File scheme",
			input:    "file:///etc/passwd",
			expected: "",
		},
		{
			name:     "Javascript scheme",
			input:    "javascript:alert(1)",
			expected: "",
		},

		// Edge cases
		{
			name:     "Too long",
			input:    "https://" + strings.Repeat("a", 2048), // 8 + 2048 > 2048
			expected: "",
		},
		{
			name:     "Max length exactly",
			input:    "https://" + strings.Repeat("a", 2048-8), // 8 + 2040 = 2048
			expected: "https://" + strings.Repeat("a", 2040),
		},
		{
			name:     "Malformed URL parse error",
			input:    "https://example.com/%zz",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.ExtractURL(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractURL(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestValidator_ExtractURL_DisallowedSchemeByConfig(t *testing.T) {
	v := &Validator{
		allowedSchemes: map[string]bool{
			"http":  false,
			"https": false,
		},
	}

	got := v.ExtractURL("https://example.com")
	if got != "" {
		t.Fatalf("ExtractURL() = %q, want empty string", got)
	}
}

func TestReadURL(t *testing.T) {
	original := clipboardReadAll
	t.Cleanup(func() {
		clipboardReadAll = original
	})

	t.Run("clipboard read error", func(t *testing.T) {
		clipboardReadAll = func() (string, error) {
			return "", errors.New("clipboard unavailable")
		}

		if got := ReadURL(); got != "" {
			t.Fatalf("ReadURL() = %q, want empty string", got)
		}
	})

	t.Run("clipboard text is valid URL", func(t *testing.T) {
		clipboardReadAll = func() (string, error) {
			return "  https://example.com/file.zip  ", nil
		}

		if got := ReadURL(); got != "https://example.com/file.zip" {
			t.Fatalf("ReadURL() = %q, want %q", got, "https://example.com/file.zip")
		}
	})
}
