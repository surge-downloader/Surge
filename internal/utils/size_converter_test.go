package utils

import (
	"testing"
)

func TestConvertBytesToHumanReadable(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		// Zero and basic bytes
		{"zero bytes", 0, "0 B"},
		{"single byte", 1, "1 B"},
		{"small bytes", 500, "500 B"},
		{"max bytes before KB", 1023, "1023 B"},

		// Kilobytes
		{"exactly 1 KB", 1024, "1.0 KB"},
		{"1.5 KB", 1536, "1.5 KB"},
		{"large KB", 512 * 1024, "512.0 KB"},

		// Megabytes
		{"exactly 1 MB", 1024 * 1024, "1.0 MB"},
		{"1.5 MB", 1536 * 1024, "1.5 MB"},
		{"100 MB", 100 * 1024 * 1024, "100.0 MB"},

		// Gigabytes
		{"exactly 1 GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"2.5 GB", int64(2.5 * 1024 * 1024 * 1024), "2.5 GB"},

		// Terabytes
		{"exactly 1 TB", 1024 * 1024 * 1024 * 1024, "1.0 TB"},

		// Petabytes
		{"exactly 1 PB", 1024 * 1024 * 1024 * 1024 * 1024, "1.0 PB"},

		// Edge cases
		{"large file size", 1500000000, "1.4 GB"}, // 1.5 billion bytes ≈ 1.4 GB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertBytesToHumanReadable(tt.bytes)
			if got != tt.expected {
				t.Errorf(
					"ConvertBytesToHumanReadable(%d) = %q, want %q",
					tt.bytes,
					got,
					tt.expected,
				)
			}
		})
	}
}

func TestConvertBytesToHumanReadable_Consistency(t *testing.T) {
	// Ensure the function returns consistent results for the same input
	input := int64(1073741824) // 1 GB
	result1 := ConvertBytesToHumanReadable(input)
	result2 := ConvertBytesToHumanReadable(input)

	if result1 != result2 {
		t.Errorf("Inconsistent results: %q vs %q", result1, result2)
	}
}

func TestConvertBytesToHumanReadable_BoundaryValues(t *testing.T) {
	// Test values right at unit boundaries
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"1023 bytes", 1023, "1023 B"},
		{"1024 bytes", 1024, "1.0 KB"},
		{"1025 bytes", 1025, "1.0 KB"},
		{"1023 KB", 1023 * 1024, "1023.0 KB"},
		{"1024 KB", 1024 * 1024, "1.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertBytesToHumanReadable(tt.bytes)
			if got != tt.expected {
				t.Errorf(
					"ConvertBytesToHumanReadable(%d) = %q, want %q",
					tt.bytes,
					got,
					tt.expected,
				)
			}
		})
	}
}
