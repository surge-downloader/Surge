package cmd

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "surge-cmd-test-*")
	if err == nil {
		_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	}

	code := m.Run()

	if err == nil {
		_ = os.RemoveAll(tmpDir)
	}
	os.Exit(code)
}
