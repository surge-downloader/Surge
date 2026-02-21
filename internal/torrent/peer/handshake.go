package peer

import (
	"bytes"
	"fmt"
	"io"
)

const protocolString = "BitTorrent protocol"

const (
	extensionReservedByte = 5
	extensionReservedBit  = byte(0x10)
)

type Handshake struct {
	InfoHash [20]byte
	PeerID   [20]byte
	Reserved [8]byte
}

func (h Handshake) SupportsExtensionProtocol() bool {
	return h.Reserved[extensionReservedByte]&extensionReservedBit != 0
}

func WriteHandshake(w io.Writer, h Handshake) error {
	if _, err := w.Write([]byte{byte(len(protocolString))}); err != nil {
		return err
	}
	if _, err := w.Write([]byte(protocolString)); err != nil {
		return err
	}
	reserved := h.Reserved
	// Always advertise BEP10 support so peers can use extension messages (ut_pex).
	reserved[extensionReservedByte] |= extensionReservedBit
	if _, err := w.Write(reserved[:]); err != nil {
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
	var h Handshake
	if _, err := io.ReadFull(r, h.Reserved[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, h.InfoHash[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, h.PeerID[:]); err != nil {
		return nil, err
	}
	return &h, nil
}
