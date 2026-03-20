package state

import (
	"database/sql"
	"embed"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

//go:embed schemas/001_initial.sql
var schemaFS embed.FS

// DB wraps a sql.DB with Axiom-specific state operations.
// It includes a write mutex to serialize SQLite writes, preventing SQLITE_BUSY
// errors when multiple goroutines dispatch tasks concurrently. SQLite WAL mode
// supports concurrent reads but still serializes writes; the mutex ensures
// writes are serialized at the Go level before reaching SQLite.
type DB struct {
	conn *sql.DB
	wmu  sync.Mutex // serializes all write operations
}

// NewDB opens a SQLite database at dbPath with WAL mode, busy timeout,
// and connection pool settings appropriate for concurrent use.
func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// WAL mode for concurrent reads
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	// Busy timeout so concurrent writers wait instead of failing immediately.
	// This is a safety net in case the write mutex is bypassed via Conn().
	if _, err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	conn.SetMaxOpenConns(10)

	return &DB{conn: conn}, nil
}

// WriteExec serializes a write operation through the write mutex.
// All INSERT/UPDATE/DELETE operations should use this to prevent SQLITE_BUSY.
func (db *DB) WriteExec(query string, args ...interface{}) (sql.Result, error) {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	return db.conn.Exec(query, args...)
}

// RunMigrations reads and executes the embedded initial schema SQL.
func (db *DB) RunMigrations() error {
	schema, err := schemaFS.ReadFile("schemas/001_initial.sql")
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}
	db.wmu.Lock()
	defer db.wmu.Unlock()
	if _, err := db.conn.Exec(string(schema)); err != nil {
		return fmt.Errorf("exec migration: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Conn returns the underlying *sql.DB for direct access if needed.
func (db *DB) Conn() *sql.DB {
	return db.conn
}
