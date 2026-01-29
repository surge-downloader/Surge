package download

import (
	"os"
	"path/filepath"
	"testing"
)

// Tests requested by user: ensure "file (1)" is preserved if it doesn't exist
func TestUniqueFilePath_Preservation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-filename-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Scenario 1: "file (1).txt" comes in, and DOES NOT exist.
	// Expected: Return "file (1).txt" as is.
	inputFile := filepath.Join(tmpDir, "file (1).txt")
	got := uniqueFilePath(inputFile)
	if got != inputFile {
		t.Errorf("Scenario 1 Failed: Expected '%s', got '%s'. Should preserve unique filename.", inputFile, got)
	}

	// Scenario 2: "file (1).txt" comes in, and DOES exist.
	// Expected: Return "file (2).txt" (incrementing the counter).
	if err := os.WriteFile(inputFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	expectedFile2 := filepath.Join(tmpDir, "file (2).txt")
	got2 := uniqueFilePath(inputFile)
	if got2 != expectedFile2 {
		t.Errorf("Scenario 2 Failed: Expected '%s', got '%s'. Should increment existing counter.", expectedFile2, got2)
	}

	// Scenario 3: "file (2).txt" ALSO exists.
	// Expected: Return "file (3).txt".
	if err := os.WriteFile(expectedFile2, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	expectedFile3 := filepath.Join(tmpDir, "file (3).txt")
	got3 := uniqueFilePath(inputFile) // Input is still "file (1).txt"
	if got3 != expectedFile3 {
		t.Errorf("Scenario 3 Failed: Expected '%s', got '%s'. Should skip to next available.", expectedFile3, got3)
	}
}

// Additional robustness tests
func TestUniqueFilePath_WhitespaceParsing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-filename-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// file with trailing space "file (1) .txt" caused issues before
	baseFile := filepath.Join(tmpDir, "file (1).txt")
	if err := os.WriteFile(baseFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// If we ask for "file (1).txt", we get "file (2).txt" (Normal)
	// If we ask for "file (1) .txt" (note space), and that file exists...
	spaceFile := filepath.Join(tmpDir, "file (1) .txt")
	if err := os.WriteFile(spaceFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Logic should parse "file (1) " -> clean "file (1)" -> base "file ", counter 2 -> "file (2).txt"
	// So uniqueFilePath(".../file (1) .txt") -> ".../file (2).txt"

	got := uniqueFilePath(spaceFile)
	expected := filepath.Join(tmpDir, "file (2).txt")

	// Note: "file (2).txt" does NOT exist yet.
	if got != expected {
		t.Errorf("Whitespace Parsing Failed: Expected '%s', got '%s'", expected, got)
	}
}
