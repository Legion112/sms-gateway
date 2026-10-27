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

// MessageKey returns the storage primary key for a modem-local message.
// Timestamp is included because modem storage indices are reused for new SMS.
func MessageKey(modem, id, receivedAt string) string {
	if receivedAt == "" {
		return modem + ":" + id
	}
	return modem + ":" + id + ":" + receivedAt
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
		path = path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	store := &Store{db: db}
	if err := store.migrateLegacyIDs(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrateConcatSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
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

// ReplaceMessage updates an existing row and clears delivery state (modem index reused).
func (s *Store) ReplaceMessage(msg Message) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`UPDATE messages SET from_number = ?, body = ?, received_at = ? WHERE id = ?`,
		msg.FromNumber, msg.Body, msg.ReceivedAt, msg.ID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM deliveries WHERE message_id = ?`, msg.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// GetMessage returns a stored message by id.
func (s *Store) GetMessage(id string) (Message, bool, error) {
	var msg Message
	err := s.db.QueryRow(
		`SELECT id, modem, from_number, body, received_at FROM messages WHERE id = ?`, id,
	).Scan(&msg.ID, &msg.Modem, &msg.FromNumber, &msg.Body, &msg.ReceivedAt)
	if err == sql.ErrNoRows {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}
	return msg, true, nil
}

// NeedsDelivery reports whether channel still needs a successful delivery for messageID.
func (s *Store) NeedsDelivery(messageID, channel string) (bool, error) {
	superseded, err := s.IsSuperseded(messageID)
	if err != nil {
		return false, err
	}
	if superseded {
		return false, nil
	}
	var sentAt sql.NullString
	err = s.db.QueryRow(
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

func (s *Store) migrateLegacyIDs() error {
	var legacy int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE instr(id, ':') = 0`).Scan(&legacy); err != nil {
		return err
	}
	if legacy == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`UPDATE deliveries
		 SET message_id = (
		   SELECT m.modem || ':' || m.id FROM messages m
		   WHERE m.id = deliveries.message_id LIMIT 1
		 )
		 WHERE instr(message_id, ':') = 0`,
	); err != nil {
		return fmt.Errorf("migrate delivery ids: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE messages SET id = modem || ':' || id WHERE instr(id, ':') = 0`,
	); err != nil {
		return fmt.Errorf("migrate message ids: %w", err)
	}
	return tx.Commit()
}

// IsFullyDelivered reports whether messageID has at least one delivery attempt
// recorded and every recorded channel succeeded. A message with no delivery rows
// is not considered delivered, so its modem slot is never freed on that basis.
func (s *Store) IsFullyDelivered(messageID string) (bool, error) {
	var total, sent int
	err := s.db.QueryRow(
		`SELECT COUNT(*), COUNT(CASE WHEN sent_at IS NOT NULL AND sent_at != '' THEN 1 END)
		 FROM deliveries WHERE message_id = ?`,
		messageID,
	).Scan(&total, &sent)
	if err != nil {
		return false, err
	}
	return total > 0 && total == sent, nil
}

// HasPendingDelivery reports whether messageID has a delivery row that has not
// yet succeeded. A message with no rows at all has nothing pending: it was never
// routed to a channel, so there is no attempt to retry.
func (s *Store) HasPendingDelivery(messageID string) (bool, error) {
	var pending int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM deliveries
		 WHERE message_id = ? AND (sent_at IS NULL OR sent_at = '')`,
		messageID,
	).Scan(&pending)
	if err != nil {
		return false, err
	}
	return pending > 0, nil
}
