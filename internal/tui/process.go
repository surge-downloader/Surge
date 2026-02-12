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

			d.Downloaded = msg.Downloaded
			d.Total = msg.Total
			d.Speed = msg.Speed
			d.Elapsed = msg.Elapsed
			d.Connections = msg.ActiveConnections

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

			// Rolling average history logic
			totalSpeed := m.calcTotalSpeed()
			m.speedBuffer = append(m.speedBuffer, totalSpeed)
			if len(m.speedBuffer) > 10 {
				m.speedBuffer = m.speedBuffer[1:]
			}

			if time.Since(m.lastSpeedHistoryUpdate) >= GraphUpdateInterval {
				var avgSpeed float64
				if len(m.speedBuffer) > 0 {
					for _, s := range m.speedBuffer {
						avgSpeed += s
					}
					avgSpeed /= float64(len(m.speedBuffer))
				}
				if len(m.SpeedHistory) > 0 {
					m.SpeedHistory = append(m.SpeedHistory[1:], avgSpeed)
				}
				m.lastSpeedHistoryUpdate = time.Now()
			}

			m.UpdateListItems()
			break
		}
	}
}
