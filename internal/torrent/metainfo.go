package torrent

import (
	"bytes"
	"fmt"

	"github.com/anacrolix/torrent/metainfo"
)

func ParseTorrent(data []byte) (*TorrentMeta, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	parsedInfo, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}

	info := Info{
		Name:        parsedInfo.BestName(),
		PieceLength: parsedInfo.PieceLength,
		Pieces:      append([]byte(nil), parsedInfo.Pieces...),
		Length:      parsedInfo.Length,
	}
	for _, f := range parsedInfo.UpvertedFiles() {
		info.Files = append(info.Files, FileEntry{
			Path:   append([]string(nil), f.BestPath()...),
			Length: f.Length,
		})
	}
	if info.Name == "" {
		info.Name = parsedInfo.Name
	}
	if err := validateInfo(info); err != nil {
		return nil, err
	}

	hash := mi.HashInfoBytes()

	meta := &TorrentMeta{
		Info:      info,
		InfoHash:  hash,
		InfoBytes: append([]byte(nil), mi.InfoBytes...),
		Announce:  mi.Announce,
	}
	for _, tier := range mi.UpvertedAnnounceList() {
		if len(tier) == 0 {
			continue
		}
		meta.AnnounceList = append(meta.AnnounceList, append([]string(nil), tier...))
	}

	return meta, nil
}

func validateInfo(info Info) error {
	if info.PieceLength == 0 || len(info.Pieces) == 0 || info.Name == "" {
		return fmt.Errorf("invalid info dict")
	}
	if info.Length == 0 && len(info.Files) == 0 {
		return fmt.Errorf("missing length/files")
	}
	return nil
}
