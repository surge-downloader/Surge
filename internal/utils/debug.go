package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	debugFile *os.File
	debugOnce sync.Once
	logsDir   string
	mu        sync.RWMutex
)

// ConfigureDebug sets the directory for debug logs
func ConfigureDebug(dir string) {
	mu.Lock()
	defer mu.Unlock()
	logsDir = dir
}

// Debug writes a message to debug.log file in the configured directory
func Debug(format string, args ...any) {
	// add timestamp to each debug message
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	mu.RLock()
	dir := logsDir
	mu.RUnlock()

	// If no logs directory is configured, do nothing (or could log to stderr)
	if dir == "" {
		return
	}

	debugOnce.Do(func() {
		os.MkdirAll(dir, 0755)
		debugFile, _ = os.Create(filepath.Join(dir, fmt.Sprintf("debug-%s.log", time.Now().Format("20060102-150405"))))
	})

	if debugFile != nil {
		fmt.Fprintf(debugFile, "[%s] %s\n", timestamp, fmt.Sprintf(format, args...))
	}
}
