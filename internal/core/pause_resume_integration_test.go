package core

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
)

func waitForDownloadStatus(
	t *testing.T,
	svc *LocalDownloadService,
	id string,
	timeout time.Duration,
	predicate func(*types.DownloadStatus) bool,
) *types.DownloadStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *types.DownloadStatus
	var lastErr error

	for time.Now().Before(deadline) {
		st, err := svc.GetStatus(id)
		last = st
		lastErr = err
		if err == nil && st != nil && predicate(st) {
			return st
		}
		time.Sleep(50 * time.Millisecond)
	}

	if lastErr != nil {
		t.Fatalf("timeout waiting for status for %s; last error: %v", id, lastErr)
	}
	if last == nil {
		t.Fatalf("timeout waiting for status for %s; last status: <nil>", id)
	}
	t.Fatalf(
		"timeout waiting for status for %s; last status: status=%s downloaded=%d total=%d speed=%.4f conns=%d progress=%.3f",
		id,
		last.Status,
		last.Downloaded,
		last.TotalSize,
		last.Speed,
		last.Connections,
		last.Progress,
	)
	return nil
}

func waitForSavedStateByID(
	t *testing.T,
	id string,
	timeout time.Duration,
	predicate func(*types.DownloadState) bool,
) *types.DownloadState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *types.DownloadState
	var lastErr error

	for time.Now().Before(deadline) {
		states, err := state.LoadStates([]string{id})
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}

		s := states[id]
		if s == nil {
			lastErr = fmt.Errorf("download state %s not found yet", id)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		last = s
		if predicate(s) {
			return s
		}
		time.Sleep(50 * time.Millisecond)
	}

	if last != nil {
		t.Fatalf(
			"timeout waiting for saved state; last state: downloaded=%d total=%d elapsed=%d tasks=%d",
			last.Downloaded,
			last.TotalSize,
			last.Elapsed,
			len(last.Tasks),
		)
	}
	t.Fatalf("timeout waiting for saved state; last error: %v", lastErr)
	return nil
}

func progressFrom(downloaded, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(downloaded) * 100 / float64(total)
}

func requireProgressClose(t *testing.T, got, want float64, label string) {
	t.Helper()
	const eps = 0.001
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > eps {
		t.Fatalf("%s progress mismatch: got=%.6f want=%.6f (diff=%.6f)", label, got, want, diff)
	}
}

func newDeterministicStreamingServer(t *testing.T, fileSize int64) *testutil.StreamingMockServer {
	t.Helper()
	return testutil.NewStreamingMockServerT(
		t,
		fileSize,
		testutil.WithRangeSupport(true),
		testutil.WithLatency(10*time.Millisecond),
		// Streaming server applies ByteLatency per KB. This keeps tests deterministic
		// and provides a stable pause window without making the suite too slow.
		testutil.WithByteLatency(500*time.Microsecond),
	)
}

func forceSingleConnectionRuntime(svc *LocalDownloadService) {
	svc.settingsMu.Lock()
	defer svc.settingsMu.Unlock()

	if svc.settings == nil {
		svc.settings = config.DefaultSettings()
	}

	// Keep integration behavior deterministic:
	// - single worker connection (no hedging/stealing overlap effects),
	// - conservative health settings to avoid synthetic task cancellation.
	svc.settings.Network.MaxConnectionsPerHost = 1
	svc.settings.Performance.SlowWorkerGracePeriod = 60 * time.Second
	svc.settings.Performance.StallTimeout = 60 * time.Second
}

func TestIntegration_PauseResume_HotPath_Aggregates(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", rootDir)

	state.CloseDB()
	dbPath := filepath.Join(rootDir, "surge.db")
	state.Configure(dbPath)
	defer state.CloseDB()

	progressCh := make(chan any, 256)
	pool := download.NewWorkerPool(progressCh, 1)
	svc := NewLocalDownloadServiceWithInput(pool, progressCh)
	forceSingleConnectionRuntime(svc)
	defer func() { _ = svc.Shutdown() }()

	const fileSize = int64(96 * 1024 * 1024)
	server := newDeterministicStreamingServer(t, fileSize)
	defer server.Close()

	outputDir := t.TempDir()
	const filename = "hot-aggregate.bin"
	destPath := filepath.Join(outputDir, filename)

	id, err := svc.Add(server.URL(), outputDir, filename, nil, nil)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	start := waitForDownloadStatus(t, svc, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" &&
			st.TotalSize == fileSize &&
			st.Downloaded > 2*1024*1024 &&
			st.Speed > 0
	})

	if start.TotalSize != fileSize {
		t.Fatalf("total size mismatch before pause: got=%d want=%d", start.TotalSize, fileSize)
	}
	requireProgressClose(t, start.Progress, progressFrom(start.Downloaded, start.TotalSize), "before-pause")

	if err := svc.Pause(id); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	paused := waitForDownloadStatus(t, svc, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "paused"
	})

	if paused.Downloaded < start.Downloaded {
		t.Fatalf("downloaded moved backwards on pause: before=%d paused=%d", start.Downloaded, paused.Downloaded)
	}
	if paused.Speed != 0 {
		t.Fatalf("paused speed must be 0, got %.6f MB/s", paused.Speed)
	}
	if paused.Connections != 0 {
		t.Fatalf("paused connections must be 0, got %d", paused.Connections)
	}
	requireProgressClose(t, paused.Progress, progressFrom(paused.Downloaded, paused.TotalSize), "paused")

	time.Sleep(700 * time.Millisecond)
	pausedStable := waitForDownloadStatus(t, svc, id, 5*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "paused"
	})
	if pausedStable.Downloaded != paused.Downloaded {
		t.Fatalf("paused downloaded drifted: first=%d later=%d", paused.Downloaded, pausedStable.Downloaded)
	}
	if pausedStable.Speed != 0 {
		t.Fatalf("paused speed drifted from 0 to %.6f", pausedStable.Speed)
	}

	saved1 := waitForSavedStateByID(t, id, 25*time.Second, func(s *types.DownloadState) bool {
		return s.TotalSize == fileSize && s.Downloaded > 0 && s.Elapsed > 0 && len(s.Tasks) > 0
	})

	if saved1.Downloaded != paused.Downloaded {
		t.Fatalf("saved downloaded mismatch: saved=%d status=%d", saved1.Downloaded, paused.Downloaded)
	}
	if saved1.DestPath != destPath {
		t.Fatalf("saved dest path mismatch: got=%q want=%q", saved1.DestPath, destPath)
	}

	entry1, err := state.GetDownload(id)
	if err != nil {
		t.Fatalf("state.GetDownload failed: %v", err)
	}
	if entry1 == nil {
		t.Fatal("missing master-list entry after pause")
	}
	if entry1.Status != "paused" {
		t.Fatalf("entry status mismatch: got=%q want=paused", entry1.Status)
	}
	if entry1.Downloaded != saved1.Downloaded {
		t.Fatalf("entry downloaded mismatch: entry=%d saved=%d", entry1.Downloaded, saved1.Downloaded)
	}
	if saved1.Elapsed != entry1.TimeTaken*int64(time.Millisecond) {
		t.Fatalf(
			"elapsed persistence mismatch: saved_ns=%d entry_ms=%d expected_saved_ns=%d",
			saved1.Elapsed,
			entry1.TimeTaken,
			entry1.TimeTaken*int64(time.Millisecond),
		)
	}

	if err := svc.Resume(id); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	resumed := waitForDownloadStatus(t, svc, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" &&
			st.TotalSize == fileSize &&
			st.Downloaded >= saved1.Downloaded &&
			st.Speed > 0
	})
	if resumed.Downloaded < saved1.Downloaded {
		t.Fatalf("resumed downloaded below saved snapshot: resumed=%d saved=%d", resumed.Downloaded, saved1.Downloaded)
	}

	resumedAdvanced := waitForDownloadStatus(t, svc, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" &&
			st.Downloaded > saved1.Downloaded+1024*1024 &&
			st.Speed > 0
	})
	if resumedAdvanced.Downloaded <= saved1.Downloaded {
		t.Fatalf("resume made no forward progress: resumed=%d saved=%d", resumedAdvanced.Downloaded, saved1.Downloaded)
	}
	requireProgressClose(t, resumedAdvanced.Progress, progressFrom(resumedAdvanced.Downloaded, resumedAdvanced.TotalSize), "resumed-advanced")

	if err := svc.Pause(id); err != nil {
		t.Fatalf("second pause failed: %v", err)
	}

	paused2 := waitForDownloadStatus(t, svc, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "paused"
	})
	if paused2.Speed != 0 {
		t.Fatalf("second paused speed must be 0, got %.6f", paused2.Speed)
	}

	saved2 := waitForSavedStateByID(t, id, 25*time.Second, func(s *types.DownloadState) bool {
		return s.Downloaded > saved1.Downloaded && s.Elapsed > saved1.Elapsed
	})

	if saved2.Downloaded != paused2.Downloaded {
		t.Fatalf("second saved downloaded mismatch: saved=%d status=%d", saved2.Downloaded, paused2.Downloaded)
	}
	if saved2.Elapsed <= saved1.Elapsed {
		t.Fatalf("elapsed did not increase across pause/resume cycle: first=%d second=%d", saved1.Elapsed, saved2.Elapsed)
	}

	// Keep the failure output concrete and useful.
	t.Logf(
		"hot path aggregates: first_pause(downloaded=%d elapsed_ns=%d) second_pause(downloaded=%d elapsed_ns=%d)",
		saved1.Downloaded,
		saved1.Elapsed,
		saved2.Downloaded,
		saved2.Elapsed,
	)
}

func TestIntegration_PauseResume_ColdPath_StateContinuity(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", rootDir)

	state.CloseDB()
	dbPath := filepath.Join(rootDir, "surge.db")
	state.Configure(dbPath)
	defer state.CloseDB()

	const fileSize = int64(96 * 1024 * 1024)
	server := newDeterministicStreamingServer(t, fileSize)
	defer server.Close()

	outputDir := t.TempDir()
	const filename = "cold-continuity.bin"
	destPath := filepath.Join(outputDir, filename)

	// Service instance #1: start and pause.
	ch1 := make(chan any, 256)
	pool1 := download.NewWorkerPool(ch1, 1)
	svc1 := NewLocalDownloadServiceWithInput(pool1, ch1)
	forceSingleConnectionRuntime(svc1)

	id, err := svc1.Add(server.URL(), outputDir, filename, nil, nil)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	waitForDownloadStatus(t, svc1, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" &&
			st.TotalSize == fileSize &&
			st.Downloaded > 2*1024*1024 &&
			st.Speed > 0
	})

	if err := svc1.Pause(id); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	paused1 := waitForDownloadStatus(t, svc1, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "paused"
	})

	saved1 := waitForSavedStateByID(t, id, 25*time.Second, func(s *types.DownloadState) bool {
		return s.Downloaded == paused1.Downloaded && s.Elapsed > 0
	})

	if err := svc1.Shutdown(); err != nil {
		t.Fatalf("svc1 shutdown failed: %v", err)
	}

	// Service instance #2: cold resume from DB.
	ch2 := make(chan any, 256)
	pool2 := download.NewWorkerPool(ch2, 1)
	svc2 := NewLocalDownloadServiceWithInput(pool2, ch2)
	forceSingleConnectionRuntime(svc2)
	defer func() { _ = svc2.Shutdown() }()

	if err := svc2.Resume(id); err != nil {
		t.Fatalf("cold resume failed: %v", err)
	}

	resumed := waitForDownloadStatus(t, svc2, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" &&
			st.TotalSize == fileSize &&
			st.Downloaded >= saved1.Downloaded &&
			st.Speed > 0
	})

	if resumed.Downloaded < saved1.Downloaded {
		t.Fatalf("cold resume lost downloaded bytes: resumed=%d saved=%d", resumed.Downloaded, saved1.Downloaded)
	}
	requireProgressClose(t, resumed.Progress, progressFrom(resumed.Downloaded, resumed.TotalSize), "cold-resume")

	waitForDownloadStatus(t, svc2, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" &&
			st.Downloaded > saved1.Downloaded+1024*1024 &&
			st.Speed > 0
	})

	if err := svc2.Pause(id); err != nil {
		t.Fatalf("second pause after cold resume failed: %v", err)
	}

	paused2 := waitForDownloadStatus(t, svc2, id, 25*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "paused"
	})
	if paused2.Speed != 0 {
		t.Fatalf("paused speed after cold resume must be 0, got %.6f", paused2.Speed)
	}

	saved2 := waitForSavedStateByID(t, id, 25*time.Second, func(s *types.DownloadState) bool {
		return s.Downloaded > saved1.Downloaded && s.Elapsed > saved1.Elapsed
	})

	if saved2.DestPath != destPath {
		t.Fatalf("dest path changed across cold resume: got=%q want=%q", saved2.DestPath, destPath)
	}
	if saved2.Downloaded != paused2.Downloaded {
		t.Fatalf("saved downloaded mismatch after cold resume: saved=%d status=%d", saved2.Downloaded, paused2.Downloaded)
	}

	entry2, err := state.GetDownload(id)
	if err != nil {
		t.Fatalf("state.GetDownload failed: %v", err)
	}
	if entry2 == nil {
		t.Fatal("missing entry after second pause")
	}
	if entry2.Status != "paused" {
		t.Fatalf("entry status mismatch after cold resume: got=%q", entry2.Status)
	}
	if entry2.Downloaded != saved2.Downloaded {
		t.Fatalf("entry downloaded mismatch after cold resume: entry=%d saved=%d", entry2.Downloaded, saved2.Downloaded)
	}
	if saved2.Elapsed != entry2.TimeTaken*int64(time.Millisecond) {
		t.Fatalf(
			"elapsed mismatch after cold resume: saved_ns=%d entry_ms=%d expected_saved_ns=%d",
			saved2.Elapsed,
			entry2.TimeTaken,
			entry2.TimeTaken*int64(time.Millisecond),
		)
	}

	t.Logf(
		"cold path continuity: first_pause(downloaded=%d elapsed_ns=%d) second_pause(downloaded=%d elapsed_ns=%d)",
		saved1.Downloaded,
		saved1.Elapsed,
		saved2.Downloaded,
		saved2.Elapsed,
	)
}

func TestIntegration_PauseResume_ResumeBatchRejectsPausing(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", rootDir)

	state.CloseDB()
	dbPath := filepath.Join(rootDir, "surge.db")
	state.Configure(dbPath)
	defer state.CloseDB()

	progressCh := make(chan any, 16)
	pool := download.NewWorkerPool(progressCh, 1)
	svc := NewLocalDownloadServiceWithInput(pool, progressCh)
	defer func() { _ = svc.Shutdown() }()

	id := "resume-batch-pausing-id"
	ps := types.NewProgressState(id, 1024)
	ps.Pause()
	ps.SetPausing(true)

	pool.Add(types.DownloadConfig{
		ID:       id,
		URL:      "http://example.com/file.bin",
		Filename: "file.bin",
		State:    ps,
	})

	// Ensure the pool has tracked it before we hit ResumeBatch.
	waitForDownloadStatus(t, svc, id, 5*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "pausing" || st.Status == "paused"
	})

	errs := svc.ResumeBatch([]string{id})
	if len(errs) != 1 {
		t.Fatalf("unexpected errors length: got=%d want=1", len(errs))
	}
	if errs[0] == nil {
		t.Fatal("expected resume-batch to reject pausing download")
	}
	want := "download is still pausing, try again in a moment"
	if got := errs[0].Error(); got != want {
		t.Fatalf("unexpected error: got=%q want=%q", got, want)
	}

	st, err := svc.GetStatus(id)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if st == nil {
		t.Fatal("missing status for pausing download")
	}
	if st.Status != "pausing" {
		t.Fatalf("status changed unexpectedly after rejected resume-batch: got=%q", st.Status)
	}
	if !ps.IsPaused() {
		t.Fatal("paused flag changed unexpectedly after rejected resume-batch")
	}
	if !ps.IsPausing() {
		t.Fatal("pausing flag changed unexpectedly after rejected resume-batch")
	}

	t.Logf("resume-batch pausing rejection preserved state: id=%s status=%s err=%s", id, st.Status, errs[0])
}

func TestIntegration_PauseResume_StatusFormulaInvariants(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", rootDir)

	state.CloseDB()
	dbPath := filepath.Join(rootDir, "surge.db")
	state.Configure(dbPath)
	defer state.CloseDB()

	progressCh := make(chan any, 256)
	pool := download.NewWorkerPool(progressCh, 1)
	svc := NewLocalDownloadServiceWithInput(pool, progressCh)
	forceSingleConnectionRuntime(svc)
	defer func() { _ = svc.Shutdown() }()

	const fileSize = int64(64 * 1024 * 1024)
	server := newDeterministicStreamingServer(t, fileSize)
	defer server.Close()

	outputDir := t.TempDir()
	const filename = "formula.bin"
	id, err := svc.Add(server.URL(), outputDir, filename, nil, nil)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	active := waitForDownloadStatus(t, svc, id, 20*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" &&
			st.TotalSize == fileSize &&
			st.Downloaded > 1024*1024 &&
			st.Speed > 0
	})
	requireProgressClose(t, active.Progress, progressFrom(active.Downloaded, active.TotalSize), "active")
	if active.ETA < 0 {
		t.Fatalf("active ETA must be non-negative, got %d", active.ETA)
	}

	if err := svc.Pause(id); err != nil {
		t.Fatalf("pause failed: %v", err)
	}
	paused := waitForDownloadStatus(t, svc, id, 20*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "paused"
	})
	requireProgressClose(t, paused.Progress, progressFrom(paused.Downloaded, paused.TotalSize), "paused")
	if paused.Speed != 0 {
		t.Fatalf("paused speed must be 0, got %.6f", paused.Speed)
	}
	if paused.ETA != 0 {
		// API uses zero-value ETA when paused.
		t.Fatalf("paused ETA must be 0, got %d", paused.ETA)
	}

	// DB-only status path must preserve the same progress invariant.
	entry, err := state.GetDownload(id)
	if err != nil {
		t.Fatalf("GetDownload failed: %v", err)
	}
	if entry == nil {
		t.Fatal("expected paused entry in DB")
	}

	statuses, err := svc.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	found := false
	for _, st := range statuses {
		if st.ID != id {
			continue
		}
		found = true
		requireProgressClose(t, st.Progress, progressFrom(st.Downloaded, st.TotalSize), "list")
		if st.Status != "paused" && st.Status != "pausing" {
			t.Fatalf("list status mismatch: got=%q", st.Status)
		}
		break
	}
	if !found {
		t.Fatalf("download %s missing from List()", id)
	}

	t.Logf(
		"status invariants: active(downloaded=%d progress=%.4f eta=%d) paused(downloaded=%d progress=%.4f)",
		active.Downloaded,
		active.Progress,
		active.ETA,
		paused.Downloaded,
		paused.Progress,
	)
}

func TestIntegration_PauseResume_ConcreteSnapshotDebugString(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", rootDir)

	state.CloseDB()
	dbPath := filepath.Join(rootDir, "surge.db")
	state.Configure(dbPath)
	defer state.CloseDB()

	progressCh := make(chan any, 256)
	pool := download.NewWorkerPool(progressCh, 1)
	svc := NewLocalDownloadServiceWithInput(pool, progressCh)
	forceSingleConnectionRuntime(svc)
	defer func() { _ = svc.Shutdown() }()

	const fileSize = int64(32 * 1024 * 1024)
	server := newDeterministicStreamingServer(t, fileSize)
	defer server.Close()

	outputDir := t.TempDir()
	const filename = "snapshot-debug.bin"

	id, err := svc.Add(server.URL(), outputDir, filename, nil, nil)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	waitForDownloadStatus(t, svc, id, 15*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "downloading" && st.Downloaded > 512*1024
	})

	if err := svc.Pause(id); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	paused := waitForDownloadStatus(t, svc, id, 20*time.Second, func(st *types.DownloadStatus) bool {
		return st.Status == "paused"
	})
	saved := waitForSavedStateByID(t, id, 20*time.Second, func(s *types.DownloadState) bool {
		return s.Downloaded == paused.Downloaded && s.Elapsed > 0
	})

	entry, err := state.GetDownload(id)
	if err != nil {
		t.Fatalf("GetDownload failed: %v", err)
	}
	if entry == nil {
		t.Fatal("missing DB entry")
	}

	snapshot := fmt.Sprintf(
		"id=%s status=%s downloaded=%d total=%d progress=%.6f speed=%.6f eta=%d conns=%d saved_downloaded=%d saved_elapsed_ns=%d db_time_ms=%d tasks=%d",
		id,
		paused.Status,
		paused.Downloaded,
		paused.TotalSize,
		paused.Progress,
		paused.Speed,
		paused.ETA,
		paused.Connections,
		saved.Downloaded,
		saved.Elapsed,
		entry.TimeTaken,
		len(saved.Tasks),
	)
	if snapshot == "" {
		t.Fatal("unexpected empty snapshot")
	}
	t.Log(snapshot)
}
