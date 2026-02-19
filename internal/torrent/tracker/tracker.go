package tracker

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"time"
)

type FailureKind int

const (
	FailureUnknown FailureKind = iota
	FailureTimeout
	FailureDNS
	FailureRefused
	FailureUnreachable
)

func Announce(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	var lastErr error
	const maxAttempts = 2
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := announceOnce(announceURL, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isTimeoutErr(err) || attempt+1 >= maxAttempts {
			break
		}
		time.Sleep(time.Duration(300*(attempt+1)) * time.Millisecond)
	}
	return nil, lastErr
}

func announceOnce(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return AnnounceHTTP(announceURL, req)
	case "udp":
		return AnnounceUDP(announceURL, req)
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	type timeout interface{ Timeout() bool }
	var te timeout
	if errors.As(err, &te) {
		return te.Timeout()
	}
	return false
}

func ClassifyFailure(err error) FailureKind {
	if err == nil {
		return FailureUnknown
	}
	if isTimeoutErr(err) {
		return FailureTimeout
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return FailureDNS
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "server misbehaving"):
		return FailureDNS
	case strings.Contains(msg, "connection refused"):
		return FailureRefused
	case strings.Contains(msg, "network is unreachable"), strings.Contains(msg, "host is unreachable"), strings.Contains(msg, "no route to host"):
		return FailureUnreachable
	default:
		return FailureUnknown
	}
}

func DefaultPeerID() [20]byte {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var id [20]byte
	copy(id[:8], []byte("-SG0001-"))
	for i := 8; i < 20; i++ {
		id[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return id
}
