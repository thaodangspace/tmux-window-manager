package store

import (
	"database/sql"
	"fmt"
	"unicode/utf8"

	_ "modernc.org/sqlite" // pure-Go driver (cgo-free, keeps build-on-install simple)
)

// schemaVersion is bumped whenever the schema changes; Migrate is a no-op once
// the DB is already at this version (tracked via PRAGMA user_version).
const schemaVersion = 1

// maxField bounds untrusted text fields (prompt/latest/detail) before insert.
const maxField = 4096

// Status values an agent reports through its lifecycle.
const (
	Idle    = "idle"    // session open, not currently working
	Running = "running" // actively working on a turn
	Waiting = "waiting" // blocked on the user (permission / input)
	Ended   = "ended"   // session ended (rows are normally deleted instead)
)

// Status is one agent's current state plus the enrichment the picker shows.
// Event is not persisted in agent_status; it is only recorded in the
// append-only agent_event history when set on an Upsert.
type Status struct {
	Agent     string // claude | codex | pi
	SessionID string
	Cwd       string
	Pid       int    // resolved agent process pid; 0 = unknown
	Status    string // one of the constants above
	Detail    string // tool name / notification message
	Model     string
	Prompt    string // first user prompt of the session
	Latest    string // most recent assistant message
	UpdatedAt int64  // unix milliseconds
	Event     string // history-only: the hook event that produced this write
}

// DB wraps the sql.DB handle.
type DB struct{ sql *sql.DB }

// Open resolves the canonical path and opens (creating if needed) the DB.
func Open() (*DB, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return OpenAt(path)
}

// OpenAt opens the DB at an explicit path. WAL + a 5s busy timeout let many
// short-lived hook writers run concurrently without spurious "database is
// locked" errors.
func OpenAt(path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite serializes writes regardless; a single in-process connection
	// avoids self-contention (cross-process writers still wait via busy_timeout).
	sdb.SetMaxOpenConns(1)
	db := &DB{sql: sdb}
	if err := db.Migrate(); err != nil {
		sdb.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the underlying handle.
func (db *DB) Close() error { return db.sql.Close() }

// Migrate applies the v1 schema once. Idempotent: a second call on an
// already-migrated DB returns immediately.
func (db *DB) Migrate() error {
	var v int
	if err := db.sql.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v >= schemaVersion {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agent_status (
			agent          TEXT NOT NULL,
			session_id     TEXT NOT NULL,
			cwd            TEXT NOT NULL DEFAULT '',
			pid            INTEGER NOT NULL DEFAULT 0,
			status         TEXT NOT NULL,
			detail         TEXT NOT NULL DEFAULT '',
			model          TEXT NOT NULL DEFAULT '',
			prompt         TEXT NOT NULL DEFAULT '',
			latest_message TEXT NOT NULL DEFAULT '',
			updated_at     INTEGER NOT NULL,
			PRIMARY KEY (agent, session_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_status_cwd ON agent_status(cwd)`,
		`CREATE TABLE IF NOT EXISTS agent_event (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			agent      TEXT NOT NULL,
			session_id TEXT NOT NULL,
			event      TEXT NOT NULL,
			status     TEXT NOT NULL,
			cwd        TEXT NOT NULL DEFAULT '',
			at         INTEGER NOT NULL
		)`,
		fmt.Sprintf("PRAGMA user_version = %d", schemaVersion),
	}
	for _, s := range stmts {
		if _, err := db.sql.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// Upsert writes the agent's current status, keyed by (agent, session_id). The
// first non-empty prompt is preserved across turns; model/latest update when a
// newer non-empty value arrives. When s.Event is set, a history row is appended.
func (db *DB) Upsert(s Status) error {
	s.Detail = truncate(s.Detail)
	s.Prompt = truncate(s.Prompt)
	s.Latest = truncate(s.Latest)

	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO agent_status
		(agent, session_id, cwd, pid, status, detail, model, prompt, latest_message, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(agent, session_id) DO UPDATE SET
			cwd            = excluded.cwd,
			pid            = excluded.pid,
			status         = excluded.status,
			detail         = excluded.detail,
			model          = CASE WHEN excluded.model <> '' THEN excluded.model ELSE agent_status.model END,
			prompt         = CASE WHEN agent_status.prompt = '' THEN excluded.prompt ELSE agent_status.prompt END,
			latest_message = CASE WHEN excluded.latest_message <> '' THEN excluded.latest_message ELSE agent_status.latest_message END,
			updated_at     = excluded.updated_at`,
		s.Agent, s.SessionID, s.Cwd, s.Pid, s.Status, s.Detail,
		s.Model, s.Prompt, s.Latest, s.UpdatedAt); err != nil {
		return err
	}

	if s.Event != "" {
		if _, err := tx.Exec(
			`INSERT INTO agent_event (agent, session_id, event, status, cwd, at) VALUES (?,?,?,?,?,?)`,
			s.Agent, s.SessionID, s.Event, s.Status, s.Cwd, s.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete removes a session's status row (used on SessionEnd and to reap dead
// rows). Missing rows are not an error.
func (db *DB) Delete(agent, sessionID string) error {
	_, err := db.sql.Exec(`DELETE FROM agent_status WHERE agent = ? AND session_id = ?`, agent, sessionID)
	return err
}

// All returns every status row, oldest first.
func (db *DB) All() ([]Status, error) {
	rows, err := db.sql.Query(`SELECT agent, session_id, cwd, pid, status, detail,
		model, prompt, latest_message, updated_at FROM agent_status ORDER BY updated_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Status
	for rows.Next() {
		var s Status
		if err := rows.Scan(&s.Agent, &s.SessionID, &s.Cwd, &s.Pid, &s.Status,
			&s.Detail, &s.Model, &s.Prompt, &s.Latest, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Live returns every status row whose process is still alive (unknown pid 0 is
// treated as live), oldest first. Dead rows are filtered out and lazily reaped
// from the DB. It is the basis the picker matches against by pid.
func (db *DB) Live() ([]Status, error) {
	all, err := db.All() // ascending updated_at
	if err != nil {
		return nil, err
	}
	live := make([]Status, 0, len(all))
	var dead []Status
	for _, s := range all {
		if alive(s.Pid) {
			live = append(live, s)
		} else {
			dead = append(dead, s)
		}
	}
	for _, d := range dead {
		_ = db.Delete(d.Agent, d.SessionID) // best-effort reap
	}
	return live, nil
}

// LiveByCwd returns the newest live status per working directory (later
// updated_at wins). Used by the `status` debug command; the picker matches by
// pid via Live instead, so two agents sharing a directory stay distinct.
func (db *DB) LiveByCwd() (map[string]Status, error) {
	live, err := db.Live()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Status, len(live))
	for _, s := range live {
		if s.Cwd == "" {
			continue
		}
		out[s.Cwd] = s
	}
	return out, nil
}

// truncate bounds a field to maxField bytes without splitting a UTF-8 rune.
func truncate(s string) string {
	if len(s) <= maxField {
		return s
	}
	s = s[:maxField]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
