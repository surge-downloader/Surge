package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDBLifecycle(t *testing.T) {
	// Setup isolated environment
	tempDir := setupTestDB(t)
	defer os.RemoveAll(tempDir)
	defer CloseDB()

	// Test GetDB (should be initialized by setupTestDB)
	d, err := GetDB()
	if err != nil {
		t.Fatalf("GetDB failed: %v", err)
	}
	if d == nil {
		t.Fatal("GetDB returned nil")
	}

	// Test Singleton
	d2, err := GetDB()
	if err != nil {
		t.Fatalf("GetDB 2 failed: %v", err)
	}
	if d != d2 {
		t.Error("GetDB should return the same instance")
	}

	// Test CloseDB
	CloseDB()
	if db != nil {
		t.Error("db variable should be nil after CloseDB")
	}

	// Verify we can re-open (GetDB should re-init)
	d3, err := GetDB()
	if err != nil {
		t.Fatalf("Re-opening GetDB failed: %v", err)
	}
	if d3 == nil {
		t.Fatal("Re-opened DB is nil")
	}
	if d3 == d {
		t.Log("Re-opened DB instance address is same as old closed one (unlikely but possible if pointer reused)")
	}

	// Test tables exist
	tx, err := d3.Begin()
	if err != nil {
		t.Fatalf("Failed to begin tx: %v", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("SELECT * FROM downloads LIMIT 1")
	if err != nil {
		t.Errorf("Table 'downloads' check failed: %v", err)
	}
	_, err = tx.Exec("SELECT * FROM tasks LIMIT 1")
	if err != nil {
		t.Errorf("Table 'tasks' check failed: %v", err)
	}
}

func TestWithTx_Commit(t *testing.T) {
	tmpDir := setupTestDB(t)
	defer os.RemoveAll(tmpDir)
	defer CloseDB()

	err := withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO downloads (id, url, dest_path) VALUES (?, ?, ?)", "tx-test-1", "http://tx.com/1", "/tmp/1")
		return err
	})
	if err != nil {
		t.Fatalf("withTx failed: %v", err)
	}

	// Verify data persisted
	d, _ := GetDB()
	var url string
	err = d.QueryRow("SELECT url FROM downloads WHERE id = ?", "tx-test-1").Scan(&url)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if url != "http://tx.com/1" {
		t.Errorf("Expected 'http://tx.com/1', got '%s'", url)
	}
}

func TestWithTx_Rollback(t *testing.T) {
	tmpDir := setupTestDB(t)
	defer os.RemoveAll(tmpDir)
	defer CloseDB()

	// Ensure DB is clean
	d, _ := GetDB()
	d.Exec("DELETE FROM downloads")

	expectedErr := fmt.Errorf("intentional error")
	err := withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO downloads (id, url, dest_path) VALUES (?, ?, ?)", "tx-test-2", "http://tx.com/2", "/tmp/2")
		if err != nil {
			return err
		}
		// trigger rollback
		return expectedErr
	})

	if err != expectedErr {
		t.Fatalf("Expected error %v, got %v", expectedErr, err)
	}

	// Verify data NOT persisted
	var count int
	err = d.QueryRow("SELECT count(*) FROM downloads WHERE id = ?", "tx-test-2").Scan(&count)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 0 {
		t.Error("Transaction should have rolled back, but record found")
	}
}

func TestInitDB_createsDir(t *testing.T) {
	// Setup isolated environment but manually to check dir creation
	tempDir, err := os.MkdirTemp("", "surge-db-init-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Ensure pure state
	dbMu.Lock()
	if db != nil {
		db.Close()
		db = nil
	}
	configured = false
	dbMu.Unlock()

	// Configure
	dbPath := filepath.Join(tempDir, "surge.db")
	Configure(dbPath)

	// GetDB calls initDB
	d, err := GetDB()
	if err != nil {
		t.Fatalf("GetDB failed: %v", err)
	}
	defer d.Close()

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("Database file not created at %s", dbPath)
	}
}
