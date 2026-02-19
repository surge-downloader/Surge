package peer

import (
	"bytes"
	"net"
	"testing"
)

func TestParseExtendedHandshake(t *testing.T) {
	msg, err := MakeExtendedHandshakeMessage(map[string]byte{
		utPexExtensionName: utPexLocalMessageID,
	})
	if err != nil {
		t.Fatalf("make handshake message: %v", err)
	}
	extID, payload, err := ParseExtendedMessage(msg)
	if err != nil {
		t.Fatalf("parse extended message: %v", err)
	}
	if extID != extendedHandshakeID {
		t.Fatalf("unexpected extended id: %d", extID)
	}
	hs, err := ParseExtendedHandshake(payload)
	if err != nil {
		t.Fatalf("parse extended handshake: %v", err)
	}
	if hs.Messages[utPexExtensionName] != utPexLocalMessageID {
		t.Fatalf("ut_pex id mismatch: got %d", hs.Messages[utPexExtensionName])
	}
}

func TestMakeAndParseUTPexMessage(t *testing.T) {
	orig := []net.TCPAddr{
		{IP: net.IPv4(1, 2, 3, 4), Port: 6881},
		{IP: net.ParseIP("2001:db8::1"), Port: 51413},
	}
	msg, err := MakeUTPexMessage(3, orig)
	if err != nil {
		t.Fatalf("make ut_pex: %v", err)
	}
	extID, payload, err := ParseExtendedMessage(msg)
	if err != nil {
		t.Fatalf("parse extended: %v", err)
	}
	if extID != 3 {
		t.Fatalf("unexpected ext id: %d", extID)
	}
	peers, err := ParseUTPexPeers(payload)
	if err != nil {
		t.Fatalf("parse ut_pex payload: %v", err)
	}
	if len(peers) != len(orig) {
		t.Fatalf("unexpected peers count: got %d want %d", len(peers), len(orig))
	}
	got := map[string]bool{}
	for _, p := range peers {
		got[p.String()] = true
	}
	for _, p := range orig {
		if !got[p.String()] {
			t.Fatalf("missing peer %s", p.String())
		}
	}
}

func TestHandshakeAdvertisesExtensionProtocol(t *testing.T) {
	var ih [20]byte
	var pid [20]byte
	var buf bytes.Buffer

	if err := WriteHandshake(&buf, Handshake{InfoHash: ih, PeerID: pid}); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	got, err := ReadHandshake(&buf)
	if err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	if !got.SupportsExtensionProtocol() {
		t.Fatalf("expected extension protocol support bit to be set")
	}
}

func TestConnHandleUTPexPayload(t *testing.T) {
	c := &Conn{}
	hsMsg, err := MakeExtendedHandshakeMessage(map[string]byte{
		utPexExtensionName: 9,
	})
	if err != nil {
		t.Fatalf("make extended handshake: %v", err)
	}
	_, _, peers := c.handle(hsMsg)
	if len(peers) != 0 {
		t.Fatalf("expected no peers from handshake, got %d", len(peers))
	}

	pexMsg, err := MakeUTPexMessage(9, []net.TCPAddr{
		{IP: net.IPv4(10, 1, 2, 3), Port: 51413},
	})
	if err != nil {
		t.Fatalf("make ut_pex: %v", err)
	}
	_, _, peers = c.handle(pexMsg)
	if len(peers) != 1 {
		t.Fatalf("expected one pex peer, got %d", len(peers))
	}
	if peers[0].String() != "10.1.2.3:51413" {
		t.Fatalf("unexpected pex peer: %s", peers[0].String())
	}
}
