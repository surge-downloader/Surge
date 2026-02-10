package state

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// Helper to avoid passing *testing.T which is not available in Benchmark setup except via b.Fatalf
func setupTestDBForBench() string {
	tempDir, err := os.MkdirTemp("", "surge-bench-*")
	if err != nil {
		panic(fmt.Sprintf("Failed to create temp dir: %v", err))
	}

	dbMu.Lock()
	if db != nil {
		_ = db.Close()
		db = nil
	}
	configured = false
	dbMu.Unlock()

	dbPath := filepath.Join(tempDir, "surge.db")
	Configure(dbPath)

	if err := initDB(); err != nil {
		panic(fmt.Sprintf("Failed to init DB: %v", err))
	}

	return tempDir
}

func BenchmarkSaveState(b *testing.B) {
	tmpDir := setupTestDBForBench()
	defer func() { _ = os.RemoveAll(tmpDir) }()
	defer CloseDB()

	numTasks := 1000
	tasks := make([]types.Task, numTasks)
	for i := 0; i < numTasks; i++ {
		tasks[i] = types.Task{
			Offset: int64(i * 1000),
			Length: 1000,
		}
	}

	testURL := "https://bench.example.com/file.zip"
	testDestPath := filepath.Join(tmpDir, "benchfile.zip")
	id := uuid.New().String()

	state := &types.DownloadState{
		ID:         id,
		URL:        testURL,
		DestPath:   testDestPath,
		TotalSize:  int64(numTasks * 1000),
		Downloaded: 0,
		Tasks:      tasks,
		Filename:   "benchfile.zip",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := SaveState(testURL, testDestPath, state); err != nil {
			b.Fatalf("SaveState failed: %v", err)
		}
	}
}
