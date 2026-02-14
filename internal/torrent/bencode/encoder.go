package bencode

import (
	"bytes"
	"fmt"
	"sort"
)

func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case int:
		return encodeInt(buf, int64(t))
	case int64:
		return encodeInt(buf, t)
	case string:
		return encodeString(buf, []byte(t))
	case []byte:
		return encodeString(buf, t)
	case []any:
		return encodeList(buf, t)
	case map[string]any:
		return encodeDict(buf, t)
	default:
		return fmt.Errorf("unsupported type %T", v)
	}
}

func encodeInt(buf *bytes.Buffer, n int64) error {
	buf.WriteByte('i')
	_, _ = fmt.Fprintf(buf, "%d", n)
	buf.WriteByte('e')
	return nil
}

func encodeString(buf *bytes.Buffer, b []byte) error {
	_, _ = fmt.Fprintf(buf, "%d:", len(b))
	_, _ = buf.Write(b)
	return nil
}

func encodeList(buf *bytes.Buffer, list []any) error {
	buf.WriteByte('l')
	for _, v := range list {
		if err := encodeValue(buf, v); err != nil {
			return err
		}
	}
	buf.WriteByte('e')
	return nil
}

func encodeDict(buf *bytes.Buffer, m map[string]any) error {
	buf.WriteByte('d')
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := encodeString(buf, []byte(k)); err != nil {
			return err
		}
		if err := encodeValue(buf, m[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('e')
	return nil
}
