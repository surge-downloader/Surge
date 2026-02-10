package cmd

import (
	"net"
	"testing"
)

func requireTCPListener(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp listener unavailable: %v", err)
		return
	}
	_ = ln.Close()
}
