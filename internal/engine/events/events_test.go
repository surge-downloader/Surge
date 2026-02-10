package events

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestDownloadPausedMsg_Creation(t *testing.T) {
	msg := DownloadPausedMsg{
		DownloadID: "immediate-pause",
		Downloaded: 0,
	}

	if msg.Downloaded != 0 {
		t.Error("Immediate pause should have 0 bytes downloaded")
	}
}

// =============================================================================
// DownloadResumedMsg Tests
// =============================================================================

func TestDownloadResumedMsg_Creation(t *testing.T) {
	msg := DownloadResumedMsg{
		DownloadID: "resumed-123",
	}

	if msg.DownloadID != "resumed-123" {
		t.Errorf("Expected DownloadID 'resumed-123', got %s", msg.DownloadID)
	}
}

func TestDownloadResumedMsg_ZeroValues(t *testing.T) {
	var msg DownloadResumedMsg

	if msg.DownloadID != "" {
		t.Error("Zero value DownloadID should be empty")
	}
}

// =============================================================================
// Message Type Assertions (for interface compatibility)
// =============================================================================

func TestMessageTypes_AreDistinct(t *testing.T) {
	// Verify all message types are distinct and can be type-switched
	messages := []interface{}{
		ProgressMsg{DownloadID: "progress"},
		DownloadCompleteMsg{DownloadID: "complete"},
		DownloadErrorMsg{DownloadID: "error"},
		DownloadStartedMsg{DownloadID: "started"},
		DownloadPausedMsg{DownloadID: "paused"},
		DownloadResumedMsg{DownloadID: "resumed"},
	}

	typeNames := make(map[string]bool)
	for _, msg := range messages {
		typeName := fmt.Sprintf("%T", msg)
		if typeNames[typeName] {
			t.Errorf("Duplicate type: %s", typeName)
		}
		typeNames[typeName] = true
	}

	if len(typeNames) != 6 {
		t.Errorf("Expected 6 distinct types, got %d", len(typeNames))
	}
}

func TestMessageTypes_TypeSwitch(t *testing.T) {
	var msg interface{} = ProgressMsg{DownloadID: "test"}

	switch m := msg.(type) {
	case ProgressMsg:
		if m.DownloadID != "test" {
			t.Error("Type switch should preserve value")
		}
	default:
		t.Error("Should match ProgressMsg")
	}
}

// =============================================================================
// Channel Communication Tests
// =============================================================================

func TestProgressMsg_ChannelCommunication(t *testing.T) {
	ch := make(chan ProgressMsg, 1)

	sent := ProgressMsg{
		DownloadID: "channel-test",
		Downloaded: 1000,
		Total:      2000,
	}

	ch <- sent
	received := <-ch

	if !reflect.DeepEqual(received, sent) {
		t.Error("Message should be identical after channel send/receive")
	}
}

func TestDownloadCompleteMsg_ChannelCommunication(t *testing.T) {
	ch := make(chan DownloadCompleteMsg, 1)

	sent := DownloadCompleteMsg{
		DownloadID: "channel-complete",
		Elapsed:    5 * time.Second,
	}

	ch <- sent
	received := <-ch

	if received.DownloadID != sent.DownloadID {
		t.Error("DownloadID should match")
	}
	if received.Elapsed != sent.Elapsed {
		t.Error("Elapsed should match")
	}
}

func TestDownloadErrorMsg_ChannelCommunication(t *testing.T) {
	ch := make(chan DownloadErrorMsg, 1)

	err := errors.New("test error")
	sent := DownloadErrorMsg{
		DownloadID: "channel-error",
		Err:        err,
	}

	ch <- sent
	received := <-ch

	if received.Err.Error() != err.Error() {
		t.Error("Error should match")
	}
}

// =============================================================================
// Edge Cases and Special Characters
// =============================================================================

func TestDownloadStartedMsg_SpecialFilenames(t *testing.T) {
	testCases := []struct {
		name     string
		filename string
	}{
		{"with spaces", "my file.zip"},
		{"unicode", "文件.zip"},
		{"special chars", "file (1).zip"},
		{"very long", string(make([]byte, 255))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := DownloadStartedMsg{
				Filename: tc.filename,
			}
			if msg.Filename != tc.filename {
				t.Errorf("Filename not preserved: %s", tc.filename)
			}
		})
	}
}

func TestDownloadStartedMsg_URLVariants(t *testing.T) {
	testCases := []struct {
		name string
		url  string
	}{
		{"http", "http://example.com/file"},
		{"https", "https://example.com/file"},
		{"with port", "https://example.com:8080/file"},
		{"with query", "https://example.com/file?key=value"},
		{"with fragment", "https://example.com/file#section"},
		{"ftp", "ftp://example.com/file"},
		{"ipv4", "http://192.168.1.1/file"},
		{"ipv6", "http://[::1]/file"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msg := DownloadStartedMsg{
				URL: tc.url,
			}
			if msg.URL != tc.url {
				t.Errorf("URL not preserved: %s", tc.url)
			}
		})
	}
}

// =============================================================================
// Equality and Comparison Tests
// =============================================================================

func TestProgressMsg_Equality(t *testing.T) {
	msg1 := ProgressMsg{
		DownloadID:        "equal",
		Downloaded:        100,
		Total:             200,
		Speed:             50,
		ActiveConnections: 2,
	}
	msg2 := ProgressMsg{
		DownloadID:        "equal",
		Downloaded:        100,
		Total:             200,
		Speed:             50,
		ActiveConnections: 2,
	}

	if !reflect.DeepEqual(msg1, msg2) {
		t.Error("Identical ProgressMsg should be equal")
	}
}

func TestDownloadCompleteMsg_Equality(t *testing.T) {
	elapsed := 5 * time.Second
	msg1 := DownloadCompleteMsg{
		DownloadID: "equal",
		Filename:   "file.zip",
		Elapsed:    elapsed,
		Total:      1000,
	}
	msg2 := DownloadCompleteMsg{
		DownloadID: "equal",
		Filename:   "file.zip",
		Elapsed:    elapsed,
		Total:      1000,
	}

	if msg1 != msg2 {
		t.Error("Identical DownloadCompleteMsg should be equal")
	}
}

// Note: DownloadErrorMsg equality is tricky because error comparison
// compares pointer/interface, not value

func TestDownloadPausedMsg_Equality(t *testing.T) {
	msg1 := DownloadPausedMsg{DownloadID: "equal", Downloaded: 500}
	msg2 := DownloadPausedMsg{DownloadID: "equal", Downloaded: 500}

	if msg1 != msg2 {
		t.Error("Identical DownloadPausedMsg should be equal")
	}
}

func TestDownloadResumedMsg_Equality(t *testing.T) {
	msg1 := DownloadResumedMsg{DownloadID: "equal"}
	msg2 := DownloadResumedMsg{DownloadID: "equal"}

	if msg1 != msg2 {
		t.Error("Identical DownloadResumedMsg should be equal")
	}
}
