package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps a SQLite connection.
type DB struct {
	conn *sql.DB
	path string // absolute path to data.db, for the Settings "reveal file" action
}

// Open creates the application data directory if needed, opens the SQLite
// database, enables WAL mode, and runs the schema migrations.
func Open() (*DB, error) {
	dir, err := appDataDir()
	if err != nil {
		return nil, fmt.Errorf("store: resolve data dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}

	path := filepath.Join(dir, "data.db")
	conn, err := sql.Open("sqlite3", path+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}

	// SQLite works best with a single writer connection.
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("store: enable WAL: %w", err)
	}

	db := &DB{conn: conn, path: path}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return db, nil
}

// Close shuts down the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Path returns the absolute path to the SQLite file, so the UI can reveal it in
// the OS file manager.
func (db *DB) Path() string {
	return db.path
}

// ClearAll deletes every row from every table in one transaction, resetting the
// app to a first-run state: sessions, transcripts, preferences (which also hold
// the API keys — both the BYOK keys and the managed test-account rows, so a full
// wipe also signs the device out of the managed tier), and starred companies.
// The schema and file are left in place — GetPreferences falls back to defaults
// (KeyMode back to "byok") and GetAPIKey/GetManagedKey return empty. Messages go
// first to respect the foreign key into sessions.
func (db *DB) ClearAll() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("store: clear all: begin: %w", err)
	}
	defer tx.Rollback()

	for _, table := range []string{"messages", "sessions", "preferences", "starred_companies"} {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("store: clear %s: %w", table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: clear all: commit: %w", err)
	}
	return nil
}

// migrate creates tables that do not yet exist and backfills columns added in
// later versions. Add new statements here as the schema evolves — existing rows
// are left untouched.
func (db *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id            TEXT PRIMARY KEY,
			problem_id    TEXT NOT NULL,
			model         TEXT NOT NULL,
			started_at    DATETIME NOT NULL,
			ended_at      DATETIME,
			problem_title TEXT,
			difficulty    TEXT,
			final_code    TEXT,
			debrief       TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role       TEXT NOT NULL,
			content    TEXT NOT NULL,
			has_image  INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);`,
		`CREATE TABLE IF NOT EXISTS preferences (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS starred_companies (
			slug       TEXT PRIMARY KEY,
			starred_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
	}
	for _, s := range stmts {
		if _, err := db.conn.Exec(s); err != nil {
			return err
		}
	}

	// Backfill columns added after the original schema, for databases created by
	// earlier versions (the CREATE TABLE above only applies to fresh databases).
	migrations := []struct{ table, column, ddl string }{
		{"sessions", "problem_title", "ALTER TABLE sessions ADD COLUMN problem_title TEXT"},
		{"sessions", "difficulty", "ALTER TABLE sessions ADD COLUMN difficulty TEXT"},
		// final_code holds the candidate's final on-screen solution, extracted to
		// text by a vision call at session end; debrief caches the generated
		// post-interview scorecard JSON so re-opening it costs no tokens.
		{"sessions", "final_code", "ALTER TABLE sessions ADD COLUMN final_code TEXT"},
		{"sessions", "debrief", "ALTER TABLE sessions ADD COLUMN debrief TEXT"},
		// company (display name) and mode ("single"/"mock") are set only for
		// Company Practice sessions so history can badge them; empty otherwise.
		{"sessions", "company", "ALTER TABLE sessions ADD COLUMN company TEXT"},
		{"sessions", "mode", "ALTER TABLE sessions ADD COLUMN mode TEXT"},
	}
	for _, m := range migrations {
		if err := db.addColumnIfMissing(m.table, m.column, m.ddl); err != nil {
			return err
		}
	}
	return nil
}

// addColumnIfMissing runs an "ALTER TABLE … ADD COLUMN" statement only when the
// column is absent, keeping migrations idempotent across restarts (SQLite has no
// "ADD COLUMN IF NOT EXISTS"). The table name is a trusted constant, not user
// input, so interpolating it into the PRAGMA is safe.
func (db *DB) addColumnIfMissing(table, column, ddl string) error {
	rows, err := db.conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("store: inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("store: scan %s column info: %w", table, err)
		}
		if name == column {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: read %s column info: %w", table, err)
	}

	if _, err := db.conn.Exec(ddl); err != nil {
		return fmt.Errorf("store: add column %s.%s: %w", table, column, err)
	}
	return nil
}

// appDataDir returns ~/Library/Application Support/mogi on macOS. The app was
// previously named "ai-interviewer"; a data dir from that era is renamed once
// so existing sessions and API keys survive the rebrand.
func appDataDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "mogi")
	legacy := filepath.Join(cfg, "ai-interviewer")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if _, err := os.Stat(legacy); err == nil {
			if err := os.Rename(legacy, dir); err != nil {
				return "", fmt.Errorf("store: migrate legacy data dir: %w", err)
			}
		}
	}
	return dir, nil
}
