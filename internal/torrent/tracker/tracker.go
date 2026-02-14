package tracker

import (
	"fmt"
	"math/rand"
	"net/url"
)

func Announce(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
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

func DefaultPeerID() [20]byte {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var id [20]byte
	copy(id[:8], []byte("-SG0001-"))
	for i := 8; i < 20; i++ {
		id[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return id
}
