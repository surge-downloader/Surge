package dht

import "testing"

func TestEncodeDecodeMessage(t *testing.T) {
	msg := &Message{
		T: "aa",
		Y: krpcQuery,
		Q: "ping",
		A: map[string]any{
			"id": []byte("12345678901234567890"),
		},
	}
	enc, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	dec, err := DecodeMessage(enc)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if dec.T != "aa" || dec.Y != krpcQuery || dec.Q != "ping" {
		t.Fatalf("message mismatch")
	}
}
