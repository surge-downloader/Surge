package state

import (
	"database/sql"
	"fmt"
	"log"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	db         *sql.DB
	dbMu       sync.Mutex
	dbPath     string
	configured bool
)

// Configure sets the path for the SQLite database
func Configure(path string) {
	dbMu.Lock()
	defer dbMu.Unlock()
	dbPath = path
	configured = true
}

// InitDB initializes the SQLite database connection using the configured path
func initDB() error {
	dbMu.Lock()
	defer dbMu.Unlock()

	if db != nil {
		return nil
	}

	if !configured || dbPath == "" {
		return fmt.Errorf("state database not configured: call state.Configure() first")
	}

	// Ensure directory exists - caller should perhaps do this, but safe to do here if path is provided

	// Open database
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Create tables
	query := `
	CREATE TABLE IF NOT EXISTS downloads (
		id TEXT PRIMARY KEY,
		url TEXT NOT NULL,
		dest_path TEXT NOT NULL,
		filename TEXT,
		status TEXT,
		total_size INTEGER,
		downloaded INTEGER,
		url_hash TEXT,
		created_at INTEGER,
		paused_at INTEGER,
		completed_at INTEGER,
		time_taken INTEGER
	);

	CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		download_id TEXT,
		offset INTEGER,
		length INTEGER,
		FOREIGN KEY(download_id) REFERENCES downloads(id) ON DELETE CASCADE
	);
	`

	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	return nil
}

// CloseDB closes the database connection
func CloseDB() {
	dbMu.Lock()
	defer dbMu.Unlock()
	if db != nil {
		db.Close()
		db = nil
	}
}

// GetDB returns the database instance, initializing it if necessary
func GetDB() (*sql.DB, error) {
	if db == nil {
		if err := initDB(); err != nil {
			return nil, err
		}
	}
	return db, nil
}

// Helper to ensure DB is initialized and return it
func getDBHelper() *sql.DB {
	d, err := GetDB()
	if err != nil {
		log.Printf("State DB Error: %v", err)
		return nil
	}
	return d
}

// Transaction helper
func withTx(fn func(*sql.Tx) error) error {
	d := getDBHelper()
	if d == nil {
		return fmt.Errorf("database not initialized")
	}

	tx, err := d.Begin()
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
