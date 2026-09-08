package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_init.sql
var initSchema string

type DB struct {
	*sql.DB
}

// SystemUser constants
const (
	SystemUserID       = "user_system"
	SystemUsername     = "system"
	SystemDisplayName  = "chatTUI System"
	GlobalRoomID       = "room_global"
	GlobalRoomName     = "global"
)

func Open(dbPath string) (*DB, error) {
	// Enable WAL mode, foreign keys, and busy timeout
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	sqldb.SetMaxOpenConns(1) // SQLite works best with 1 writer connection in WAL mode
	sqldb.SetMaxIdleConns(1)

	db := &DB{sqldb}
	if err := db.migrate(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	if err := db.initDefaults(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("init defaults failed: %w", err)
	}

	return db, nil
}

func (db *DB) migrate() error {
	_, err := db.Exec(initSchema)
	return err
}

func (db *DB) initDefaults() error {
	now := time.Now().UTC()

	// Ensure system user exists
	_, err := db.Exec(`
		INSERT OR IGNORE INTO users (id, username, display_name, password_hash, user_color, custom_status, presence_state, last_seen_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, SystemUserID, SystemUsername, SystemDisplayName, "SYSTEM_NO_LOGIN", "Magenta", "Core Engine", "online", now, now)
	if err != nil {
		return fmt.Errorf("failed to insert system user: %w", err)
	}

	// Ensure global room exists
	_, err = db.Exec(`
		INSERT OR IGNORE INTO rooms (id, name, description, is_private, password_hash, owner_id, is_temporary, expires_at, created_at)
		VALUES (?, ?, ?, 0, NULL, ?, 0, NULL, ?)
	`, GlobalRoomID, GlobalRoomName, "Global chat for all chatTUI users", SystemUserID, now)
	if err != nil {
		return fmt.Errorf("failed to insert global room: %w", err)
	}

	return nil
}
