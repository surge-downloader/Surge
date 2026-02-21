package tui

import "testing"

func TestSettingsVisibleRangeBounds(t *testing.T) {
	start, end := settingsVisibleRange(20, 19, 8)
	if start != 12 || end != 20 {
		t.Fatalf("expected [12,20), got [%d,%d)", start, end)
	}
}

func TestSettingsVisibleRangeCentersSelection(t *testing.T) {
	start, end := settingsVisibleRange(20, 10, 8)
	if start != 6 || end != 14 {
		t.Fatalf("expected [6,14), got [%d,%d)", start, end)
	}
}

func TestSettingsVisibleRangeHandlesSmallWindowOrTotals(t *testing.T) {
	start, end := settingsVisibleRange(4, 3, 10)
	if start != 0 || end != 4 {
		t.Fatalf("expected [0,4), got [%d,%d)", start, end)
	}
}
