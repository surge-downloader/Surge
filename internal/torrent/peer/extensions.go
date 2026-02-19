package peer

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/surge-downloader/surge/internal/torrent/bencode"
)

const (
	extendedHandshakeID = byte(0)
	utPexExtensionName  = "ut_pex"
	utPexLocalMessageID = byte(1)
)

type ExtendedHandshake struct {
	Messages map[string]byte
}

func MakeExtendedHandshakeMessage(messages map[string]byte) (*Message, error) {
	m := make(map[string]any, len(messages))
	for name, id := range messages {
		m[name] = int(id)
	}
	payload, err := bencode.Encode(map[string]any{
		"m": m,
		"v": "surge",
	})
	if err != nil {
		return nil, err
	}
	extPayload := make([]byte, 1+len(payload))
	extPayload[0] = extendedHandshakeID
	copy(extPayload[1:], payload)
	return &Message{ID: MsgExtended, Payload: extPayload}, nil
}

func ParseExtendedMessage(msg *Message) (extID byte, payload []byte, err error) {
	if msg == nil || msg.ID != MsgExtended {
		return 0, nil, fmt.Errorf("invalid extended message")
	}
	if len(msg.Payload) < 1 {
		return 0, nil, fmt.Errorf("invalid extended payload")
	}
	return msg.Payload[0], msg.Payload[1:], nil
}

func ParseExtendedHandshake(payload []byte) (ExtendedHandshake, error) {
	v, err := bencode.Decode(payload)
	if err != nil {
		return ExtendedHandshake{}, err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return ExtendedHandshake{}, fmt.Errorf("invalid extended handshake root")
	}
	out := ExtendedHandshake{Messages: make(map[string]byte)}
	mraw, ok := root["m"].(map[string]any)
	if !ok {
		return out, nil
	}
	for name, idRaw := range mraw {
		switch id := idRaw.(type) {
		case int64:
			if id < 0 || id > 255 {
				continue
			}
			out.Messages[name] = byte(id)
		case int:
			if id < 0 || id > 255 {
				continue
			}
			out.Messages[name] = byte(id)
		}
	}
	return out, nil
}

func ParseUTPexPeers(payload []byte) ([]net.TCPAddr, error) {
	v, err := bencode.Decode(payload)
	if err != nil {
		return nil, err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid ut_pex payload")
	}
	var peers []net.TCPAddr
	if added, ok := root["added"].([]byte); ok {
		peers = append(peers, parseCompactPeers(added, 6)...)
	}
	if added6, ok := root["added6"].([]byte); ok {
		peers = append(peers, parseCompactPeers(added6, 18)...)
	}
	return peers, nil
}

func MakeUTPexMessage(extID byte, peers []net.TCPAddr) (*Message, error) {
	if extID == 0 {
		return nil, fmt.Errorf("invalid ut_pex extension id")
	}
	var added []byte
	var added6 []byte
	for _, p := range peers {
		if p.Port <= 0 || p.Port > 65535 || p.IP == nil {
			continue
		}
		if ipv4 := p.IP.To4(); ipv4 != nil {
			entry := make([]byte, 6)
			copy(entry[:4], ipv4)
			binary.BigEndian.PutUint16(entry[4:6], uint16(p.Port))
			added = append(added, entry...)
			continue
		}
		if ipv6 := p.IP.To16(); ipv6 != nil {
			entry := make([]byte, 18)
			copy(entry[:16], ipv6)
			binary.BigEndian.PutUint16(entry[16:18], uint16(p.Port))
			added6 = append(added6, entry...)
		}
	}
	body := make(map[string]any, 2)
	if len(added) > 0 {
		body["added"] = added
	}
	if len(added6) > 0 {
		body["added6"] = added6
	}
	encoded, err := bencode.Encode(body)
	if err != nil {
		return nil, err
	}
	extPayload := make([]byte, 1+len(encoded))
	extPayload[0] = extID
	copy(extPayload[1:], encoded)
	return &Message{ID: MsgExtended, Payload: extPayload}, nil
}

func parseCompactPeers(data []byte, stride int) []net.TCPAddr {
	if stride <= 0 || len(data) < stride {
		return nil
	}
	var peers []net.TCPAddr
	for i := 0; i+stride <= len(data); i += stride {
		ipLen := stride - 2
		port := int(binary.BigEndian.Uint16(data[i+ipLen : i+stride]))
		if port <= 0 {
			continue
		}
		ip := make(net.IP, ipLen)
		copy(ip, data[i:i+ipLen])
		peers = append(peers, net.TCPAddr{IP: ip, Port: port})
	}
	return peers
}
