package torrent

import (
	"fmt"

	"github.com/anacrolix/torrent/metainfo"
)

type Magnet struct {
	InfoHash    [20]byte
	InfoHashOK  bool
	Trackers    []string
	DisplayName string
}

func ParseMagnet(raw string) (*Magnet, error) {
	m, err := metainfo.ParseMagnetUri(raw)
	if err != nil {
		return nil, err
	}
	if m.InfoHash == ([20]byte{}) {
		return nil, fmt.Errorf("missing or invalid infohash")
	}

	out := &Magnet{
		Trackers:    append([]string(nil), m.Trackers...),
		DisplayName: m.DisplayName,
		InfoHashOK:  true,
	}
	copy(out.InfoHash[:], m.InfoHash[:])
	return out, nil
}
