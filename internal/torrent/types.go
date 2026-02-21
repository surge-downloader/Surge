package torrent

type FileEntry struct {
	Path   []string
	Length int64
}

type Info struct {
	Name        string
	PieceLength int64
	Pieces      []byte
	Length      int64
	Files       []FileEntry
}

func (i Info) TotalLength() int64 {
	if i.Length > 0 {
		return i.Length
	}
	var total int64
	for _, f := range i.Files {
		total += f.Length
	}
	return total
}

type TorrentMeta struct {
	Announce     string
	AnnounceList [][]string
	Info         Info
	InfoHash     [20]byte
	InfoBytes    []byte
}
