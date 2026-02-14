package dht

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
)

const (
	krpcQuery    = "q"
	krpcResponse = "r"
	krpcError    = "e"
)

type NodeID [20]byte

func NewNodeID() (NodeID, error) {
	var id NodeID
	if _, err := rand.Read(id[:]); err != nil {
		return id, err
	}
	return id, nil
}

func (id NodeID) String() string {
	return fmt.Sprintf("%x", id[:])
}

type Peer struct {
	IP   net.IP
	Port int
}

func ParseCompactNodes(b []byte) ([]*Node, error) {
	if len(b)%26 != 0 {
		return nil, fmt.Errorf("invalid nodes length")
	}
	out := make([]*Node, 0, len(b)/26)
	for i := 0; i < len(b); i += 26 {
		var id NodeID
		copy(id[:], b[i:i+20])
		ip := net.IPv4(b[i+20], b[i+21], b[i+22], b[i+23])
		port := int(binary.BigEndian.Uint16(b[i+24 : i+26]))
		out = append(out, &Node{
			id:   id,
			addr: &net.UDPAddr{IP: ip, Port: port},
		})
	}
	return out, nil
}

func ParseCompactPeers(b []byte) ([]Peer, error) {
	if len(b)%6 != 0 {
		return nil, fmt.Errorf("invalid peers length")
	}
	out := make([]Peer, 0, len(b)/6)
	for i := 0; i < len(b); i += 6 {
		ip := net.IPv4(b[i], b[i+1], b[i+2], b[i+3])
		port := int(binary.BigEndian.Uint16(b[i+4 : i+6]))
		out = append(out, Peer{IP: ip, Port: port})
	}
	return out, nil
}
