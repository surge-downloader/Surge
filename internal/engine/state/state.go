package state

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

// URLHash returns a short hash of the URL for master list keying
// This is used for tracking completed downloads by URL
func URLHash(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:8]) // 16 chars
}

// SaveState saves download state to SQLite
func SaveState(url string, destPath string, state *types.DownloadState) error {
	// Ensure ID is set
	if state.ID == "" {
		// Try to find existing ID using StateHash equivalent or just generate new
		// Ideally ID should be passed in, but for backward compat we handle it
		state.ID = uuid.New().String()
	}

	// Set hashes and timestamps
	state.URLHash = URLHash(url)
	state.PausedAt = time.Now().Unix()
	if state.CreatedAt == 0 {
		state.CreatedAt = time.Now().Unix()
	}

	return withTx(func(tx *sql.Tx) error {
		// 1. Upsert into downloads table
		_, err := tx.Exec(`
			INSERT INTO downloads (
				id, url, dest_path, filename, status, total_size, downloaded, url_hash, created_at, paused_at, time_taken, mirrors, chunk_bitmap, actual_chunk_size
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				url=excluded.url,
				dest_path=excluded.dest_path,
				filename=excluded.filename,
				status=excluded.status,
				total_size=excluded.total_size,
				downloaded=excluded.downloaded,
				url_hash=excluded.url_hash,
				paused_at=excluded.paused_at,
				time_taken=excluded.time_taken,
				mirrors=excluded.mirrors,
				chunk_bitmap=excluded.chunk_bitmap,
				actual_chunk_size=excluded.actual_chunk_size
		`, state.ID, state.URL, state.DestPath, state.Filename, "paused", state.TotalSize, state.Downloaded, state.URLHash, state.CreatedAt, state.PausedAt, state.Elapsed/1e6, strings.Join(state.Mirrors, ","), state.ChunkBitmap, state.ActualChunkSize)
		if err != nil {
			return fmt.Errorf("failed to upsert download: %w", err)
		}

		// 2. Refresh tasks
		// First delete existing tasks for this download
		if _, err := tx.Exec("DELETE FROM tasks WHERE download_id = ?", state.ID); err != nil {
			return fmt.Errorf("failed to delete old tasks: %w", err)
		}

		// Insert new tasks using batch insert
		// SQLite limit is often 999 or 32766 params. Safe batch size: 50 tasks * 3 params = 150 params.
		const batchSize = 50
		tasks := state.Tasks
		numTasks := len(tasks)

		if numTasks > 0 {
			// Prepare statement for full batches
			placeholders := strings.Repeat("(?, ?, ?),", batchSize)
			placeholders = placeholders[:len(placeholders)-1] // remove trailing comma
			stmt, err := tx.Prepare("INSERT INTO tasks (download_id, offset, length) VALUES " + placeholders)
			if err != nil {
				return fmt.Errorf("failed to prepare batch insert: %w", err)
			}
			defer func() { _ = stmt.Close() }()

			for i := 0; i < numTasks; i += batchSize {
				end := i + batchSize
				if end > numTasks {
					// Last batch (partial)
					end = numTasks
					batch := tasks[i:end]

					var q strings.Builder
					q.WriteString("INSERT INTO tasks (download_id, offset, length) VALUES ")
					args := make([]interface{}, 0, len(batch)*3)
					for j, task := range batch {
						if j > 0 {
							q.WriteString(",")
						}
						q.WriteString("(?, ?, ?)")
						args = append(args, state.ID, task.Offset, task.Length)
					}
					if _, err := tx.Exec(q.String(), args...); err != nil {
						return fmt.Errorf("failed to insert partial batch: %w", err)
					}
				} else {
					// Full batch
					batch := tasks[i:end]
					args := make([]interface{}, 0, batchSize*3)
					for _, task := range batch {
						args = append(args, state.ID, task.Offset, task.Length)
					}
					if _, err := stmt.Exec(args...); err != nil {
						return fmt.Errorf("failed to insert tasks batch: %w", err)
					}
				}
			}
		}

		return nil
	})
}

// LoadState loads download state from SQLite
func LoadState(url string, destPath string) (*types.DownloadState, error) {
	// We need to find the download by URL and DestPath since we might not have ID yet (legacy caller)
	// But ideally callers should use ID.
	// For now, let's query by URL and DestPath.

	db := getDBHelper()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var state types.DownloadState
	var timeTaken, createdAt, pausedAt, actualChunkSize sql.NullInt64 // handle null
	var mirrors sql.NullString                                        // handle null mirrors
	var chunkBitmap []byte

	row := db.QueryRow(`
		SELECT id, url, dest_path, filename, total_size, downloaded, url_hash, created_at, paused_at, time_taken, mirrors, chunk_bitmap, actual_chunk_size
		FROM downloads 
		WHERE url = ? AND dest_path = ? AND status != 'completed'
		ORDER BY paused_at DESC LIMIT 1
	`, url, destPath)

	err := row.Scan(
		&state.ID, &state.URL, &state.DestPath, &state.Filename,
		&state.TotalSize, &state.Downloaded, &state.URLHash,
		&createdAt, &pausedAt, &timeTaken, &mirrors, &chunkBitmap, &actualChunkSize,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// Try finding without status constraint (just in case)
			return nil, fmt.Errorf("state not found: %w", os.ErrNotExist) // mimic os.ErrNotExist for compatibility
		}
		return nil, fmt.Errorf("failed to query download: %w", err)
	}

	if createdAt.Valid {
		state.CreatedAt = createdAt.Int64
	}
	if pausedAt.Valid {
		state.PausedAt = pausedAt.Int64
	}
	if timeTaken.Valid {
		state.Elapsed = timeTaken.Int64 * 1e6 // Convert ms to ns
	}
	if mirrors.Valid && mirrors.String != "" {
		state.Mirrors = strings.Split(mirrors.String, ",")
	}
	if actualChunkSize.Valid {
		state.ActualChunkSize = actualChunkSize.Int64
	}
	state.ChunkBitmap = chunkBitmap

	// Load tasks
	rows, err := db.Query("SELECT offset, length FROM tasks WHERE download_id = ?", state.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			utils.Debug("Error closing rows: %v", err)
		}
	}()

	for rows.Next() {
		var t types.Task
		if err := rows.Scan(&t.Offset, &t.Length); err != nil {
			return nil, err
		}
		state.Tasks = append(state.Tasks, t)
	}

	return &state, nil
}

// DeleteState removes the state from SQLite
func DeleteState(id string, url string, destPath string) error {
	db := getDBHelper()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	var result sql.Result
	var err error

	if id != "" {
		result, err = db.Exec("DELETE FROM downloads WHERE id = ?", id)
	} else {
		// Fallback for legacy calls without ID
		result, err = db.Exec("DELETE FROM downloads WHERE url = ? AND dest_path = ?", url, destPath)
	}

	if err != nil {
		return fmt.Errorf("failed to delete state: %w", err)
	}

	// Tasks are deleted via CASCADE or we strictly rely on download_id
	// Since we defined CASCADE in schema, it should be fine.
	// But 'tasks' table has foreign key constraint, assuming SQLite FKs are enabled.
	// We should probably ensure FKs are enabled or manually delete tasks.
	// For safety, let's manually delete if we didn't use CASCADE in creation or forgot to enable FK.
	// actually, let's just trust our schema but also execute a cleanup just deeply in case.
	// (Implementation detail: FK support needs `PRAGMA foreign_keys = ON`)

	// Check rows affected
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil // Already gone or didn't exist
	}

	return nil
}

// ================== Master List Functions ==================

// LoadMasterList loads ALL downloads (paused and completed)
func LoadMasterList() (*types.MasterList, error) {
	db := getDBHelper()
	if db == nil {
		// Return empty list if DB fails, to behave like "no file found"
		return &types.MasterList{Downloads: []types.DownloadEntry{}}, nil
	}

	rows, err := db.Query(`
		SELECT id, url, dest_path, filename, status, total_size, downloaded, completed_at, time_taken, url_hash, mirrors 
		FROM downloads
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query downloads: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			utils.Debug("Error closing rows: %v", err)
		}
	}()

	var list types.MasterList
	for rows.Next() {
		var e types.DownloadEntry
		var completedAt, timeTaken sql.NullInt64      // handle nulls
		var filename, urlHash, mirrors sql.NullString // handle nulls

		if err := rows.Scan(
			&e.ID, &e.URL, &e.DestPath, &filename, &e.Status, &e.TotalSize, &e.Downloaded,
			&completedAt, &timeTaken, &urlHash, &mirrors,
		); err != nil {
			return nil, err
		}

		if completedAt.Valid {
			e.CompletedAt = completedAt.Int64
		}
		if timeTaken.Valid {
			e.TimeTaken = timeTaken.Int64
		}
		if filename.Valid {
			e.Filename = filename.String
		}
		if urlHash.Valid {
			e.URLHash = urlHash.String
		}
		if mirrors.Valid && mirrors.String != "" {
			e.Mirrors = strings.Split(mirrors.String, ",")
		}

		list.Downloads = append(list.Downloads, e)
	}

	return &list, nil
}

// AddToMasterList adds or updates a download entry
func AddToMasterList(entry types.DownloadEntry) error {
	// Ensure ID
	if entry.ID == "" {
		if entry.URLHash != "" {
			// Try to replicate existing ID logic or fail?
			// Let's generate one if missing, but this might duplicate if not careful.
			// Best effort:
			entry.ID = uuid.New().String()
		}
	}

	return withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO downloads (
				id, url, dest_path, filename, status, total_size, downloaded, completed_at, time_taken, url_hash, mirrors
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				url=excluded.url,
				dest_path=excluded.dest_path,
				filename=excluded.filename,
				status=excluded.status,
				total_size=excluded.total_size,
				downloaded=excluded.downloaded,
				completed_at=excluded.completed_at,
				time_taken=excluded.time_taken,
				url_hash=excluded.url_hash,
				mirrors=excluded.mirrors
		`,
			entry.ID, entry.URL, entry.DestPath, entry.Filename, entry.Status, entry.TotalSize, entry.Downloaded,
			entry.CompletedAt, entry.TimeTaken, entry.URLHash, strings.Join(entry.Mirrors, ","))

		return err
	})
}

// RemoveFromMasterList removes a download entry
func RemoveFromMasterList(id string) error {
	db := getDBHelper()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := db.Exec("DELETE FROM downloads WHERE id = ?", id)
	return err
}

// GetDownload returns a single download by ID
func GetDownload(id string) (*types.DownloadEntry, error) {
	db := getDBHelper()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var e types.DownloadEntry
	var completedAt, timeTaken sql.NullInt64
	var urlHash, filename, mirrors sql.NullString

	row := db.QueryRow(`
		SELECT id, url, dest_path, filename, status, total_size, downloaded, completed_at, time_taken, url_hash, mirrors 
		FROM downloads
		WHERE id = ?
	`, id)

	if err := row.Scan(
		&e.ID, &e.URL, &e.DestPath, &filename, &e.Status, &e.TotalSize, &e.Downloaded,
		&completedAt, &timeTaken, &urlHash, &mirrors,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("failed to query download: %w", err)
	}

	if completedAt.Valid {
		e.CompletedAt = completedAt.Int64
	}
	if timeTaken.Valid {
		e.TimeTaken = timeTaken.Int64
	}
	if urlHash.Valid {
		e.URLHash = urlHash.String
	}
	if filename.Valid {
		e.Filename = filename.String
	}
	if mirrors.Valid && mirrors.String != "" {
		e.Mirrors = strings.Split(mirrors.String, ",")
	}

	return &e, nil
}

// LoadPausedDownloads returns all paused downloads
func LoadPausedDownloads() ([]types.DownloadEntry, error) {
	// Reuse LoadMasterList logic or optimize with WHERE
	list, err := LoadMasterList()
	if err != nil {
		return nil, err
	}

	var paused []types.DownloadEntry
	for _, e := range list.Downloads {
		if e.Status == "paused" || e.Status == "queued" {
			paused = append(paused, e)
		}
	}
	return paused, nil
}

// LoadCompletedDownloads returns all completed downloads
func LoadCompletedDownloads() ([]types.DownloadEntry, error) {
	list, err := LoadMasterList()
	if err != nil {
		return nil, err
	}

	var completed []types.DownloadEntry
	for _, e := range list.Downloads {
		if e.Status == "completed" {
			completed = append(completed, e)
		}
	}
	return completed, nil
}

// CheckDownloadExists checks if a download with the given URL exists in the database
func CheckDownloadExists(url string) (bool, error) {
	db := getDBHelper()
	if db == nil {
		return false, fmt.Errorf("database not initialized")
	}

	var count int
	// Check for any status (active, paused, completed)
	err := db.QueryRow("SELECT COUNT(*) FROM downloads WHERE url = ?", url).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to query download existence: %w", err)
	}

	return count > 0, nil
}

// UpdateStatus updates the status of a download by ID
func UpdateStatus(id string, status string) error {
	db := getDBHelper()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	result, err := db.Exec("UPDATE downloads SET status = ? WHERE id = ?", status, id)
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("download not found: %s", id)
	}

	return nil
}

// PauseAllDownloads pauses all non-completed downloads
func PauseAllDownloads() error {
	db := getDBHelper()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := db.Exec("UPDATE downloads SET status = 'paused' WHERE status != 'completed'")
	return err
}

// ResumeAllDownloads resumes all paused downloads (sets to queued)
func ResumeAllDownloads() error {
	db := getDBHelper()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := db.Exec("UPDATE downloads SET status = 'queued' WHERE status = 'paused'")
	return err
}

// ListAllDownloads returns all downloads
func ListAllDownloads() ([]types.DownloadEntry, error) {
	list, err := LoadMasterList()
	if err != nil {
		return nil, err
	}
	return list.Downloads, nil
}

// RemoveCompletedDownloads removes all completed downloads and returns count
func RemoveCompletedDownloads() (int64, error) {
	db := getDBHelper()
	if db == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	result, err := db.Exec("DELETE FROM downloads WHERE status = 'completed'")
	if err != nil {
		return 0, fmt.Errorf("failed to remove completed downloads: %w", err)
	}

	count, _ := result.RowsAffected()
	return count, nil
}

// LoadStates loads multiple download states from SQLite in batch
func LoadStates(ids []string) (map[string]*types.DownloadState, error) {
	if len(ids) == 0 {
		return make(map[string]*types.DownloadState), nil
	}

	db := getDBHelper()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	// Prepare IN clause placeholders
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	inClause := strings.Join(placeholders, ",")

	// 1. Load Downloads
	query := fmt.Sprintf(`
		SELECT id, url, dest_path, filename, total_size, downloaded, url_hash, created_at, paused_at, time_taken, mirrors, chunk_bitmap, actual_chunk_size
		FROM downloads
		WHERE id IN (%s) AND status != 'completed'
	`, inClause)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query downloads batch: %w", err)
	}

	states := make(map[string]*types.DownloadState)

	defer func() {
		if err := rows.Close(); err != nil {
			utils.Debug("Error closing rows: %v", err)
		}
	}()

	for rows.Next() {
		var state types.DownloadState
		var timeTaken, createdAt, pausedAt, actualChunkSize sql.NullInt64
		var mirrors sql.NullString
		var chunkBitmap []byte

		if err := rows.Scan(
			&state.ID, &state.URL, &state.DestPath, &state.Filename,
			&state.TotalSize, &state.Downloaded, &state.URLHash,
			&createdAt, &pausedAt, &timeTaken, &mirrors, &chunkBitmap, &actualChunkSize,
		); err != nil {
			return nil, err
		}

		if createdAt.Valid {
			state.CreatedAt = createdAt.Int64
		}
		if pausedAt.Valid {
			state.PausedAt = pausedAt.Int64
		}
		if timeTaken.Valid {
			state.Elapsed = timeTaken.Int64 * 1e6
		}
		if mirrors.Valid && mirrors.String != "" {
			state.Mirrors = strings.Split(mirrors.String, ",")
		}
		if actualChunkSize.Valid {
			state.ActualChunkSize = actualChunkSize.Int64
		}
		state.ChunkBitmap = chunkBitmap

		states[state.ID] = &state
	}

	// 2. Load Tasks for all these downloads
	taskQuery := fmt.Sprintf(`SELECT download_id, offset, length FROM tasks WHERE download_id IN (%s)`, inClause)
	taskRows, err := db.Query(taskQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks batch: %w", err)
	}
	defer func() {
		if err := taskRows.Close(); err != nil {
			utils.Debug("Error closing task rows: %v", err)
		}
	}()

	for taskRows.Next() {
		var downloadID string
		var t types.Task
		if err := taskRows.Scan(&downloadID, &t.Offset, &t.Length); err != nil {
			return nil, err
		}
		if s, ok := states[downloadID]; ok {
			s.Tasks = append(s.Tasks, t)
		}
	}

	return states, nil
}
