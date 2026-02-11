//go:build !torrent

package download

import (
	"context"
	"fmt"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TorrentDownload(_ context.Context, _ *types.DownloadConfig) error {
	return fmt.Errorf("torrent support not built (build with tag 'torrent')")
}
