package tracker

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"time"
)

const (
	udpProtoID     uint64 = 0x41727101980
	actionConnect  uint32 = 0
	actionAnnounce uint32 = 1
)

func AnnounceUDP(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "udp" {
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}

	addr, err := net.ResolveUDPAddr("udp", u.Host)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	connID, err := udpConnect(conn)
	if err != nil {
		return nil, err
	}
	return udpAnnounce(conn, connID, req)
}

func udpConnect(conn *net.UDPConn) (uint64, error) {
	tx := rand.Uint32()
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, udpProtoID)
	_ = binary.Write(&buf, binary.BigEndian, uint32(actionConnect))
	_ = binary.Write(&buf, binary.BigEndian, tx)

	if _, err := conn.Write(buf.Bytes()); err != nil {
		return 0, err
	}

	resp := make([]byte, 16)
	if _, err := conn.Read(resp); err != nil {
		return 0, err
	}
	action := binary.BigEndian.Uint32(resp[0:4])
	if action != actionConnect {
		return 0, fmt.Errorf("invalid connect action: %d", action)
	}
	if binary.BigEndian.Uint32(resp[4:8]) != tx {
		return 0, fmt.Errorf("transaction id mismatch")
	}
	return binary.BigEndian.Uint64(resp[8:16]), nil
}

func udpAnnounce(conn *net.UDPConn, connID uint64, req AnnounceRequest) (*AnnounceResponse, error) {
	tx := rand.Uint32()
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, connID)
	_ = binary.Write(&buf, binary.BigEndian, uint32(actionAnnounce))
	_ = binary.Write(&buf, binary.BigEndian, tx)
	_, _ = buf.Write(req.InfoHash[:])
	_, _ = buf.Write(req.PeerID[:])
	_ = binary.Write(&buf, binary.BigEndian, req.Downloaded)
	_ = binary.Write(&buf, binary.BigEndian, req.Left)
	_ = binary.Write(&buf, binary.BigEndian, req.Uploaded)
	// event
	var ev uint32
	switch req.Event {
	case "started":
		ev = 2
	case "stopped":
		ev = 3
	case "completed":
		ev = 1
	default:
		ev = 0
	}
	_ = binary.Write(&buf, binary.BigEndian, ev)
	_ = binary.Write(&buf, binary.BigEndian, uint32(0)) // IP address (default)
	_ = binary.Write(&buf, binary.BigEndian, rand.Uint32())
	if req.NumWant <= 0 {
		req.NumWant = -1
	}
	_ = binary.Write(&buf, binary.BigEndian, int32(req.NumWant))
	_ = binary.Write(&buf, binary.BigEndian, uint16(req.Port))

	if _, err := conn.Write(buf.Bytes()); err != nil {
		return nil, err
	}

	// response length is variable; read into buffer
	resp := make([]byte, 1500)
	n, err := conn.Read(resp)
	if err != nil {
		return nil, err
	}
	resp = resp[:n]
	if len(resp) < 20 {
		return nil, fmt.Errorf("short announce response")
	}
	action := binary.BigEndian.Uint32(resp[0:4])
	if action != actionAnnounce {
		return nil, fmt.Errorf("invalid announce action: %d", action)
	}
	if binary.BigEndian.Uint32(resp[4:8]) != tx {
		return nil, fmt.Errorf("transaction id mismatch")
	}
	interval := int(binary.BigEndian.Uint32(resp[8:12]))
	peers := parseCompactPeers(resp[20:])

	return &AnnounceResponse{
		Interval: interval,
		Peers:    peers,
	}, nil
}
