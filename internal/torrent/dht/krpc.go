package dht

import (
	"fmt"

	"github.com/surge-downloader/surge/internal/torrent/bencode"
)

type Message struct {
	T string
	Y string
	Q string
	A map[string]any
	R map[string]any
	E []any
}

func DecodeMessage(data []byte) (*Message, error) {
	val, err := bencode.Decode(data)
	if err != nil {
		return nil, err
	}
	root, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid krpc message")
	}
	msg := &Message{}
	if t, ok := root["t"].([]byte); ok {
		msg.T = string(t)
	}
	if y, ok := root["y"].([]byte); ok {
		msg.Y = string(y)
	}
	if q, ok := root["q"].([]byte); ok {
		msg.Q = string(q)
	}
	if a, ok := root["a"].(map[string]any); ok {
		msg.A = a
	}
	if r, ok := root["r"].(map[string]any); ok {
		msg.R = r
	}
	if e, ok := root["e"].([]any); ok {
		msg.E = e
	}
	return msg, nil
}

func EncodeMessage(msg *Message) ([]byte, error) {
	root := map[string]any{
		"t": []byte(msg.T),
		"y": []byte(msg.Y),
	}
	if msg.Y == krpcQuery {
		root["q"] = []byte(msg.Q)
		if msg.A != nil {
			root["a"] = msg.A
		}
	} else if msg.Y == krpcResponse {
		if msg.R != nil {
			root["r"] = msg.R
		}
	} else if msg.Y == krpcError {
		if msg.E != nil {
			root["e"] = msg.E
		}
	}
	return bencode.Encode(root)
}
