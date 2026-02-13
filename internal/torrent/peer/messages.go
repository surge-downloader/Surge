package peer

import (
	"bytes"
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
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, err
	}
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
	length := uint32(1 + len(msg.Payload))
	if msg.ID == 255 {
		length = 0
	}
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return err
	}
	if length == 0 {
		return nil
	}
	if _, err := w.Write([]byte{msg.ID}); err != nil {
		return err
	}
	if len(msg.Payload) > 0 {
		_, err := w.Write(msg.Payload)
		return err
	}
	return nil
}

func MakeRequest(index, begin, length uint32) *Message {
	buf := bytes.NewBuffer(nil)
	_ = binary.Write(buf, binary.BigEndian, index)
	_ = binary.Write(buf, binary.BigEndian, begin)
	_ = binary.Write(buf, binary.BigEndian, length)
	return &Message{ID: MsgRequest, Payload: buf.Bytes()}
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
