package testutil

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// NewHTTPServer starts an httptest server bound to IPv4 to avoid IPv6 listener issues in sandboxed environments.
func NewHTTPServer(handler http.Handler) *httptest.Server {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return httptest.NewServer(handler)
	}

	srv := &httptest.Server{
		Listener: ln,
		Config: &http.Server{
			Handler: handler,
		},
	}
	srv.Start()
	return srv
}

// NewHTTPServerT starts an httptest server bound to IPv4 and skips the test if binding fails.
func NewHTTPServerT(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp4 listener unavailable: %v", err)
		return nil
	}

	srv := &httptest.Server{
		Listener: ln,
		Config: &http.Server{
			Handler: handler,
		},
	}
	srv.Start()
	return srv
}
