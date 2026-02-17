package peer

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	MsgChoke         = 0
	MsgUnchoke       = 1
	MsgInterested    = 2
	MsgNotInterested = 3
	MsgHave          = 4
	MsgBitfield      = 5
	MsgRequest       = 6
	MsgPiece         = 7
	MsgCancel        = 8
)

type Message struct {
	ID      byte
	Payload []byte
}

func ReadMessage(r io.Reader) (*Message, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lenBuf[:])
	if length == 0 {
		return &Message{ID: 255}, nil
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return &Message{ID: buf[0], Payload: buf[1:]}, nil
}

func WriteMessage(w io.Writer, msg *Message) error {
	if msg == nil {
		return fmt.Errorf("nil msg")
	}
	if msg.ID == 255 {
		_, err := w.Write([]byte{0, 0, 0, 0})
		return err
	}

	length := uint32(1 + len(msg.Payload))
	buf := make([]byte, 4+length)
	binary.BigEndian.PutUint32(buf[:4], length)
	buf[4] = msg.ID
	if len(msg.Payload) > 0 {
		copy(buf[5:], msg.Payload)
	}
	_, err := w.Write(buf)
	return err
}

func MakeRequest(index, begin, length uint32) *Message {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], length)
	return &Message{ID: MsgRequest, Payload: payload}
}

func MakeHave(index uint32) *Message {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, index)
	return &Message{ID: MsgHave, Payload: payload}
}

func MakePiece(index, begin uint32, block []byte) *Message {
	payload := make([]byte, 8+len(block))
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	if len(block) > 0 {
		copy(payload[8:], block)
	}
	return &Message{ID: MsgPiece, Payload: payload}
}

func ParseHave(msg *Message) (index uint32, err error) {
	if msg.ID != MsgHave || len(msg.Payload) < 4 {
		return 0, fmt.Errorf("invalid have msg")
	}
	return binary.BigEndian.Uint32(msg.Payload[0:4]), nil
}

func ParsePiece(msg *Message) (index, begin uint32, block []byte, err error) {
	if msg.ID != MsgPiece || len(msg.Payload) < 8 {
		return 0, 0, nil, fmt.Errorf("invalid piece msg")
	}
	index = binary.BigEndian.Uint32(msg.Payload[0:4])
	begin = binary.BigEndian.Uint32(msg.Payload[4:8])
	block = msg.Payload[8:]
	return index, begin, block, nil
}

func ParseRequest(msg *Message) (index, begin, length uint32, err error) {
	if msg.ID != MsgRequest || len(msg.Payload) < 12 {
		return 0, 0, 0, fmt.Errorf("invalid request msg")
	}
	index = binary.BigEndian.Uint32(msg.Payload[0:4])
	begin = binary.BigEndian.Uint32(msg.Payload[4:8])
	length = binary.BigEndian.Uint32(msg.Payload[8:12])
	return index, begin, length, nil
}
