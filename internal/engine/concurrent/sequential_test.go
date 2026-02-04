package concurrent

import (
	"testing"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestSequentialVsParallelChunking(t *testing.T) {
	// Setup RuntimeConfig
	minChunk := int64(2 * 1024 * 1024) // 2MB

	parallelConfig := &types.RuntimeConfig{
		SequentialDownload: false,
		MinChunkSize:       minChunk,
	}

	sequentialConfig := &types.RuntimeConfig{
		SequentialDownload: true,
		MinChunkSize:       minChunk,
	}

	totalSize := int64(100 * 1024 * 1024) // 100MB
	numConns := 4

	// Test Parallel: Should use large shards (FileSize / NumConns)
	dParallel := &ConcurrentDownloader{Runtime: parallelConfig}
	chunkSizeParallel := dParallel.determineChunkSize(totalSize, numConns)

	expectedParallel := totalSize / int64(numConns) // 25MB
	// It might be aligned, so check approx equality
	if chunkSizeParallel < expectedParallel-4096 || chunkSizeParallel > expectedParallel+4096 {
		t.Errorf("Parallel: expected approx %d, got %d", expectedParallel, chunkSizeParallel)
	}

	// Test Sequential: Should use MinChunkSize
	dSequential := &ConcurrentDownloader{Runtime: sequentialConfig}
	chunkSizeSeq := dSequential.determineChunkSize(totalSize, numConns)

	if chunkSizeSeq != minChunk {
		t.Errorf("Sequential: expected %d, got %d", minChunk, chunkSizeSeq)
	}
}

func TestTaskGenerationRequestOrder(t *testing.T) {
	// Verify that tasks are generated in increasing order
	fileSize := int64(10 * 1024 * 1024) // 10MB
	chunkSize := int64(2 * 1024 * 1024) // 2MB

	tasks := createTasks(fileSize, chunkSize)

	if len(tasks) != 5 {
		t.Errorf("Expected 5 tasks, got %d", len(tasks))
	}

	for i, task := range tasks {
		expectedOffset := int64(i) * chunkSize
		if task.Offset != expectedOffset {
			t.Errorf("Task %d: expected offset %d, got %d", i, expectedOffset, task.Offset)
		}
	}
}
