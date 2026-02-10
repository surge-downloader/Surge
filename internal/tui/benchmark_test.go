package tui

import (
	"fmt"
	"testing"
)

func BenchmarkGetFilteredDownloads(b *testing.B) {
	// Setup
	m := RootModel{
		activeTab:   TabQueued,
		searchQuery: "test",
	}

	// Create many downloads
	for i := 0; i < 10000; i++ {
		filename := fmt.Sprintf("Download_Test_File_%d.zip", i)
		dm := NewDownloadModel(fmt.Sprintf("%d", i), "http://example.com", filename, 1000)
		m.downloads = append(m.downloads, dm)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.getFilteredDownloads()
	}
}
