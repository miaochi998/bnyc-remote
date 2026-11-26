// Package store persists users, devices and audit records in SQLite.
// Schema follows docs/00-方案设计.md; secrets are stored encrypted (R8).
package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// User is an account.
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Disabled    bool   `json:"disabled"`
	CreatedAt   string `json:"created_at"`
}

// Device is an address-book entry.
type Device struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	RemoteID  string `json:"remote_id"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// Secret is only filled when explicitly requested (decrypted at handler).
	Secret string `json:"password,omitempty"`
}

// Open opens (and migrates) the SQLite database at path.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name  TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'user',
  disabled      INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  remote_id    TEXT NOT NULL,
  secret_nonce TEXT NOT NULL,
  secret_cipher TEXT NOT NULL,
  note         TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  UNIQUE(user_id, remote_id)
);
CREATE TABLE IF NOT EXISTS audit_log (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id    INTEGER,
  action     TEXT NOT NULL,
  detail     TEXT NOT NULL DEFAULT '',
  ip         TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);
`)
	return err
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// CreateUser inserts a user and returns its id.
func (s *Store) CreateUser(username, passwordHash, displayName, role string) (int64, error) {
	t := now()
	res, err := s.db.Exec(`INSERT INTO users(username,password_hash,display_name,role,created_at,updated_at)
		VALUES(?,?,?,?,?,?)`, username, passwordHash, displayName, role, t, t)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetUserByUsername returns a user with its password hash.
func (s *Store) GetUserByUsername(username string) (*User, string, error) {
	var u User
	var hash string
	var disabled int
	err := s.db.QueryRow(`SELECT id,username,display_name,role,disabled,created_at,password_hash
		FROM users WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &disabled, &u.CreatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	u.Disabled = disabled != 0
	return &u, hash, nil
}

// GetUserByID returns a user without hash.
func (s *Store) GetUserByID(id int64) (*User, error) {
	var u User
	var disabled int
	err := s.db.QueryRow(`SELECT id,username,display_name,role,disabled,created_at FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &disabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Disabled = disabled != 0
	return &u, nil
}

// ListUsers returns all users (admin only).
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id,username,display_name,role,disabled,created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var disabled int
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &disabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Disabled = disabled != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPassword updates a user's password hash.
func (s *Store) SetPassword(userID int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=?, updated_at=? WHERE id=?`, hash, now(), userID)
	return err
}

// CreateDevice adds an address-book entry (secret already encrypted).
func (s *Store) CreateDevice(userID int64, name, remoteID, nonce, cipherText, note string) (int64, error) {
	t := now()
	res, err := s.db.Exec(`INSERT INTO devices(user_id,name,remote_id,secret_nonce,secret_cipher,note,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, userID, name, remoteID, nonce, cipherText, note, t, t)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListDevices returns a user's devices with encrypted fields.
func (s *Store) ListDevices(userID int64) ([]Device, error) {
	rows, err := s.db.Query(`SELECT id,user_id,name,remote_id,secret_nonce,secret_cipher,note,created_at,updated_at
		FROM devices WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		var nonce, cipherText string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.RemoteID, &nonce, &cipherText, &d.Note, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Secret = nonce + ":" + cipherText
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDevice returns one device (encrypted fields in Secret).
func (s *Store) GetDevice(userID, id int64) (*Device, error) {
	var d Device
	var nonce, cipherText string
	err := s.db.QueryRow(`SELECT id,user_id,name,remote_id,secret_nonce,secret_cipher,note,created_at,updated_at
		FROM devices WHERE user_id=? AND id=?`, userID, id).
		Scan(&d.ID, &d.UserID, &d.Name, &d.RemoteID, &nonce, &cipherText, &d.Note, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Secret = nonce + ":" + cipherText
	return &d, nil
}

// UpdateDevice updates name/note and optionally the secret.
func (s *Store) UpdateDevice(userID, id int64, name, remoteID, note, nonce, cipherText string) error {
	if nonce == "" {
		_, err := s.db.Exec(`UPDATE devices SET name=?,remote_id=?,note=?,updated_at=? WHERE user_id=? AND id=?`,
			name, remoteID, note, now(), userID, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE devices SET name=?,remote_id=?,note=?,secret_nonce=?,secret_cipher=?,updated_at=? WHERE user_id=? AND id=?`,
		name, remoteID, note, nonce, cipherText, now(), userID, id)
	return err
}

// DeleteDevice removes an entry.
func (s *Store) DeleteDevice(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM devices WHERE user_id=? AND id=?`, userID, id)
	return err
}

// Audit appends an audit record (best effort).
func (s *Store) Audit(userID int64, action, detail, ip string) {
	_, _ = s.db.Exec(`INSERT INTO audit_log(user_id,action,detail,ip,created_at) VALUES(?,?,?,?,?)`,
		userID, action, detail, ip, now())
}

// CountUsers returns the number of users.
func (s *Store) CountUsers() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}
