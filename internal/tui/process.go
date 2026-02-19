package tui

import (
	"time"

	"github.com/surge-downloader/surge/internal/engine/events"
)

func (m *RootModel) processProgressMsg(msg events.ProgressMsg) {
	for _, d := range m.downloads {
		if d.ID == msg.DownloadID {
			if d.done || d.paused {
				break
			}

			prevDownloaded := d.Downloaded
			d.Downloaded = msg.Downloaded
			d.Total = msg.Total
			d.Speed = msg.Speed
			d.Elapsed = msg.Elapsed
			d.Connections = msg.ActiveConnections
			d.PeerDiscovered = msg.PeerDiscovered
			d.PeerPending = msg.PeerPending
			d.PeerDialAttempts = msg.PeerDialAttempts
			d.PeerDialSuccess = msg.PeerDialSuccess
			d.PeerDialFailures = msg.PeerDialFailures
			d.PeerInbound = msg.PeerInbound

			// Keep "Resuming..." visible until we observe actual transfer.
			if d.resuming && (d.Speed > 0 || d.Downloaded > prevDownloaded) {
				d.resuming = false
			}

			// Update Chunk State if provided
			if msg.BitmapWidth > 0 && len(msg.ChunkBitmap) > 0 {
				if d.state != nil && msg.Total > 0 {
					d.state.SetTotalSize(msg.Total)
				}
				// We only get bitmap, no progress array (to save bandwidth)
				// State needs to be updated carefully
				if d.state != nil {
					d.state.RestoreBitmap(msg.ChunkBitmap, msg.ActualChunkSize)
				}
				if d.state != nil && len(msg.ChunkProgress) > 0 {
					d.state.SetChunkProgress(msg.ChunkProgress)
				}
			}

			if d.Total > 0 {
				percentage := float64(d.Downloaded) / float64(d.Total)
				d.progress.SetPercent(percentage)
			}

			// Update speed graph history with EMA smoothing for smooth transitions
			if time.Since(m.lastSpeedHistoryUpdate) >= GraphUpdateInterval {
				totalSpeed := m.calcTotalSpeed()
				// EMA smooth against previous graph point for visual continuity
				var smoothed float64
				if len(m.SpeedHistory) > 0 {
					prev := m.SpeedHistory[len(m.SpeedHistory)-1]
					const graphAlpha = 0.3 // Graph smoothing factor
					smoothed = graphAlpha*totalSpeed + (1-graphAlpha)*prev
				} else {
					smoothed = totalSpeed
				}
				if len(m.SpeedHistory) > 0 {
					m.SpeedHistory = append(m.SpeedHistory[1:], smoothed)
				}
				m.lastSpeedHistoryUpdate = time.Now()
			}

			m.UpdateListItems()
			break
		}
	}
}
