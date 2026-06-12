package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS messages (
  id          TEXT PRIMARY KEY,
  modem       TEXT NOT NULL,
  from_number TEXT NOT NULL,
  body        TEXT NOT NULL,
  received_at TEXT NOT NULL,
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS deliveries (
  message_id TEXT NOT NULL,
  channel    TEXT NOT NULL,
  sent_at    TEXT,
  error      TEXT,
  PRIMARY KEY (message_id, channel)
);
`

// Message is a persisted inbound SMS.
type Message struct {
	ID         string
	Modem      string
	FromNumber string
	Body       string
	ReceivedAt string
}

// Store persists messages and per-channel delivery status.
type Store struct {
	db *sql.DB
}

// Open opens or creates the SQLite database at path.
func Open(path string) (*Store, error) {
	if path != "" && path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create storage dir: %w", err)
			}
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// InsertMessage stores a message if id is new. Returns inserted=true when a new row was created.
func (s *Store) InsertMessage(msg Message) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO messages (id, modem, from_number, body, received_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.Modem, msg.FromNumber, msg.Body, msg.ReceivedAt, now,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// NeedsDelivery reports whether channel still needs a successful delivery for messageID.
func (s *Store) NeedsDelivery(messageID, channel string) (bool, error) {
	var sentAt sql.NullString
	err := s.db.QueryRow(
		`SELECT sent_at FROM deliveries WHERE message_id = ? AND channel = ?`,
		messageID, channel,
	).Scan(&sentAt)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !sentAt.Valid || sentAt.String == "", nil
}

// MarkDelivered records successful delivery to channel.
func (s *Store) MarkDelivered(messageID, channel string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO deliveries (message_id, channel, sent_at, error)
		 VALUES (?, ?, ?, NULL)
		 ON CONFLICT(message_id, channel) DO UPDATE SET sent_at = excluded.sent_at, error = NULL`,
		messageID, channel, now,
	)
	return err
}

// MarkDeliveryFailed records a failed delivery attempt.
func (s *Store) MarkDeliveryFailed(messageID, channel, errMsg string) error {
	_, err := s.db.Exec(
		`INSERT INTO deliveries (message_id, channel, sent_at, error)
		 VALUES (?, ?, NULL, ?)
		 ON CONFLICT(message_id, channel) DO UPDATE SET error = excluded.error`,
		messageID, channel, errMsg,
	)
	return err
}

// ListMessageIDs returns stored message ids for a modem.
func (s *Store) ListMessageIDs(modem string) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM messages WHERE modem = ?`, modem)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
