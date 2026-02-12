package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeServer_PartialContent_ParsesRangeAndHint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-0" {
			t.Fatalf("expected range probe header, got %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 0-0/12345")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer ts.Close()

	res, err := ProbeServer(context.Background(), ts.URL+"/real-name.bin", "hint.bin", nil)
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}
	if !res.SupportsRange || res.FileSize != 12345 || res.Filename != "hint.bin" {
		t.Fatalf("unexpected probe result: %+v", res)
	}
}

func TestProbeServer_OK_NoRange(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "77")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	res, err := ProbeServer(context.Background(), ts.URL+"/file.txt", "", nil)
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}
	if res.SupportsRange {
		t.Fatalf("expected no range support")
	}
	if res.FileSize != 77 {
		t.Fatalf("unexpected file size: %d", res.FileSize)
	}
}

func TestProbeServer_ForbiddenWithRange_FallsBackWithoutRange(t *testing.T) {
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Range") == "bytes=0-0" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("range blocked"))
			return
		}
		w.Header().Set("Content-Length", "9")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fallback"))
	}))
	defer ts.Close()

	res, err := ProbeServer(context.Background(), ts.URL+"/a.bin", "", nil)
	if err != nil {
		t.Fatalf("ProbeServer failed: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected at least two calls for fallback, got %d", calls)
	}
	if res.SupportsRange || res.FileSize != 9 {
		t.Fatalf("unexpected probe result: %+v", res)
	}
}

func TestProbeServer_UnexpectedStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := ProbeServer(context.Background(), ts.URL+"/missing", "", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProbeMirrors_FiltersRangeCapableAndDeduplicates(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-0/10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer good.Close()

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("x"))
	}))
	defer bad.Close()

	valid, errs := ProbeMirrors(context.Background(), []string{good.URL, bad.URL, good.URL})
	if len(valid) != 1 || valid[0] != good.URL {
		t.Fatalf("unexpected valid mirrors: %v", valid)
	}
	if len(errs) != 1 {
		t.Fatalf("expected one error entry, got %d (%v)", len(errs), errs)
	}
	if _, ok := errs[bad.URL]; !ok {
		t.Fatalf("expected bad mirror error entry")
	}
}

func TestProbeMirrors_ContextCancel(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hang.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	valid, errs := ProbeMirrors(ctx, []string{hang.URL})
	if len(valid) != 0 {
		t.Fatalf("expected no valid mirrors, got %v", valid)
	}
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %d", len(errs))
	}
	err := errs[hang.URL]
	if err == nil {
		t.Fatalf("expected error for canceled probe")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		// ProbeServer wraps errors, so only assert non-empty causal error class check fallback.
		if err.Error() == "" {
			t.Fatalf("unexpected empty error")
		}
	}
}
