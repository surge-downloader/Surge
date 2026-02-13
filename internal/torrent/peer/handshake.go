package peer

import (
	"bytes"
	"fmt"
	"io"
)

const protocolString = "BitTorrent protocol"

type Handshake struct {
	InfoHash [20]byte
	PeerID   [20]byte
}

func WriteHandshake(w io.Writer, h Handshake) error {
	if _, err := w.Write([]byte{byte(len(protocolString))}); err != nil {
		return err
	}
	if _, err := w.Write([]byte(protocolString)); err != nil {
		return err
	}
	// reserved bytes
	if _, err := w.Write(make([]byte, 8)); err != nil {
		return err
	}
	if _, err := w.Write(h.InfoHash[:]); err != nil {
		return err
	}
	if _, err := w.Write(h.PeerID[:]); err != nil {
		return err
	}
	return nil
}

func ReadHandshake(r io.Reader) (*Handshake, error) {
	pstrlen := make([]byte, 1)
	if _, err := io.ReadFull(r, pstrlen); err != nil {
		return nil, err
	}
	ps := make([]byte, int(pstrlen[0]))
	if _, err := io.ReadFull(r, ps); err != nil {
		return nil, err
	}
	if !bytes.Equal(ps, []byte(protocolString)) {
		return nil, fmt.Errorf("invalid protocol string")
	}
	reserved := make([]byte, 8)
	if _, err := io.ReadFull(r, reserved); err != nil {
		return nil, err
	}
	var h Handshake
	if _, err := io.ReadFull(r, h.InfoHash[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, h.PeerID[:]); err != nil {
		return nil, err
	}
	return &h, nil
}
