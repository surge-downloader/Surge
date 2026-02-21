package peer

import (
	"fmt"
	"net"

	"github.com/anacrolix/dht/v2/krpc"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/peer_protocol"
)

const (
	extendedHandshakeID = byte(peer_protocol.HandshakeExtendedID)
	utPexExtensionName  = string(peer_protocol.ExtensionNamePex)
	utPexLocalMessageID = byte(1)
)

type ExtendedHandshake struct {
	Messages map[string]byte
}

func MakeExtendedHandshakeMessage(messages map[string]byte) (*Message, error) {
	hs := peer_protocol.ExtendedHandshakeMessage{
		M: make(map[peer_protocol.ExtensionName]peer_protocol.ExtensionNumber, len(messages)),
		V: "surge",
	}
	for name, id := range messages {
		hs.M[peer_protocol.ExtensionName(name)] = peer_protocol.ExtensionNumber(id)
	}
	payload, err := bencode.Marshal(hs)
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
	var hs peer_protocol.ExtendedHandshakeMessage
	if err := bencode.Unmarshal(payload, &hs); err != nil {
		return ExtendedHandshake{}, fmt.Errorf("invalid extended handshake root")
	}
	out := ExtendedHandshake{Messages: make(map[string]byte, len(hs.M))}
	for name, id := range hs.M {
		if id == peer_protocol.ExtensionDeleteNumber {
			continue
		}
		out.Messages[string(name)] = byte(id)
	}
	return out, nil
}

func ParseUTPexPeers(payload []byte) ([]net.TCPAddr, error) {
	pex, err := peer_protocol.LoadPexMsg(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid ut_pex payload")
	}
	peers := make([]net.TCPAddr, 0, len(pex.Added)+len(pex.Added6))
	for _, p := range pex.Added {
		if p.Port <= 0 || p.IP == nil {
			continue
		}
		ip := p.IP.To4()
		if ip == nil {
			continue
		}
		peers = append(peers, net.TCPAddr{IP: append(net.IP(nil), ip...), Port: p.Port})
	}
	for _, p := range pex.Added6 {
		if p.Port <= 0 || p.IP == nil {
			continue
		}
		ip := p.IP.To16()
		if ip == nil {
			continue
		}
		peers = append(peers, net.TCPAddr{IP: append(net.IP(nil), ip...), Port: p.Port})
	}
	return peers, nil
}

func MakeUTPexMessage(extID byte, peers []net.TCPAddr) (*Message, error) {
	if extID == 0 {
		return nil, fmt.Errorf("invalid ut_pex extension id")
	}
	pex := peer_protocol.PexMsg{
		Added:  make(krpc.CompactIPv4NodeAddrs, 0, len(peers)),
		Added6: make(krpc.CompactIPv6NodeAddrs, 0, len(peers)),
	}
	for _, p := range peers {
		if p.Port <= 0 || p.Port > 65535 || p.IP == nil {
			continue
		}
		if ipv4 := p.IP.To4(); ipv4 != nil {
			pex.Added = append(pex.Added, krpc.NodeAddr{
				IP:   append(net.IP(nil), ipv4...),
				Port: p.Port,
			})
			continue
		}
		if ipv6 := p.IP.To16(); ipv6 != nil {
			pex.Added6 = append(pex.Added6, krpc.NodeAddr{
				IP:   append(net.IP(nil), ipv6...),
				Port: p.Port,
			})
		}
	}
	pp := pex.Message(peer_protocol.ExtensionNumber(extID))
	extPayload := make([]byte, 1+len(pp.ExtendedPayload))
	extPayload[0] = byte(pp.ExtendedID)
	copy(extPayload[1:], pp.ExtendedPayload)
	return &Message{ID: MsgExtended, Payload: extPayload}, nil
}
