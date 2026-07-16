package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/smsconcat"
)

const concatSchema = `
CREATE TABLE IF NOT EXISTS concat_parts (
  modem         TEXT NOT NULL,
  from_number   TEXT NOT NULL,
  concat_ref    INTEGER NOT NULL,
  part_total    INTEGER NOT NULL,
  part_num      INTEGER NOT NULL,
  modem_id      TEXT NOT NULL,
  body          TEXT NOT NULL,
  received_at   TEXT NOT NULL,
  state         TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  PRIMARY KEY (modem, from_number, concat_ref, part_total, part_num)
);

CREATE TABLE IF NOT EXISTS concat_merged (
  group_key TEXT PRIMARY KEY,
  merged_id TEXT NOT NULL,
  merged_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS superseded_messages (
  message_id TEXT PRIMARY KEY
);
`

// ConcatPart is a persisted SMS segment awaiting assembly.
type ConcatPart struct {
	Modem      string
	FromNumber string
	ConcatRef  int
	PartTotal  int
	PartNum    int
	ModemID    string
	Body       string
	ReceivedAt string
	State      string
}

func concatGroupKey(modem, from string, concatRef, partTotal int) string {
	return fmt.Sprintf("%s|%s|%d|%d", modem, from, concatRef, partTotal)
}

// ConcatMessageKey returns the storage id for a merged concatenated SMS.
func ConcatMessageKey(modem, from string, concatRef, partTotal int, receivedAt string) string {
	base := fmt.Sprintf("%s:concat:%s:%d:%d", modem, sanitizeKeyPart(from), concatRef, partTotal)
	if receivedAt == "" {
		return base
	}
	return base + ":" + receivedAt
}

func sanitizeKeyPart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return strings.NewReplacer(":", "_", "|", "_").Replace(s)
}

func (s *Store) migrateConcatSchema() error {
	if _, err := s.db.Exec(concatSchema); err != nil {
		return fmt.Errorf("migrate concat schema: %w", err)
	}
	return nil
}

// IsSuperseded reports whether a message id was merged into another delivery.
func (s *Store) IsSuperseded(messageID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM superseded_messages WHERE message_id = ?`, messageID).Scan(&n)
	return n > 0, err
}

// UpsertConcatPart stores a segment and returns whether the group is complete.
func (s *Store) UpsertConcatPart(part ConcatPart) (complete bool, parts []ConcatPart, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO concat_parts (modem, from_number, concat_ref, part_total, part_num, modem_id, body, received_at, state, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(modem, from_number, concat_ref, part_total, part_num)
		 DO UPDATE SET modem_id = excluded.modem_id, body = excluded.body, received_at = excluded.received_at, state = excluded.state`,
		part.Modem, part.FromNumber, part.ConcatRef, part.PartTotal, part.PartNum,
		part.ModemID, part.Body, part.ReceivedAt, part.State, now,
	)
	if err != nil {
		return false, nil, err
	}

	parts, err = s.listConcatParts(part.Modem, part.FromNumber, part.ConcatRef, part.PartTotal)
	if err != nil {
		return false, nil, err
	}
	return len(parts) >= part.PartTotal, parts, nil
}

func (s *Store) listConcatParts(modem, from string, concatRef, partTotal int) ([]ConcatPart, error) {
	rows, err := s.db.Query(
		`SELECT modem, from_number, concat_ref, part_total, part_num, modem_id, body, received_at, state
		 FROM concat_parts
		 WHERE modem = ? AND from_number = ? AND concat_ref = ? AND part_total = ?
		 ORDER BY part_num`,
		modem, from, concatRef, partTotal,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var parts []ConcatPart
	for rows.Next() {
		var p ConcatPart
		if err := rows.Scan(&p.Modem, &p.FromNumber, &p.ConcatRef, &p.PartTotal, &p.PartNum, &p.ModemID, &p.Body, &p.ReceivedAt, &p.State); err != nil {
			return nil, err
		}
		parts = append(parts, p)
	}
	return parts, rows.Err()
}

// MarkConcatMerged records a completed group and removes pending parts.
func (s *Store) MarkConcatMerged(modem, from string, concatRef, partTotal int, mergedID string, segmentIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	groupKey := concatGroupKey(modem, from, concatRef, partTotal)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO concat_merged (group_key, merged_id, merged_at) VALUES (?, ?, ?)`,
		groupKey, mergedID, now,
	); err != nil {
		return err
	}
	for _, id := range segmentIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO superseded_messages (message_id) VALUES (?)`, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`DELETE FROM concat_parts WHERE modem = ? AND from_number = ? AND concat_ref = ? AND part_total = ?`,
		modem, from, concatRef, partTotal,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// IsConcatMerged reports whether a concat group was already merged and forwarded.
func (s *Store) IsConcatMerged(modem, from string, concatRef, partTotal int) (bool, string, error) {
	groupKey := concatGroupKey(modem, from, concatRef, partTotal)
	var mergedID string
	err := s.db.QueryRow(`SELECT merged_id FROM concat_merged WHERE group_key = ?`, groupKey).Scan(&mergedID)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, mergedID, nil
}

// ListTimedOutConcatGroups returns incomplete groups older than the timeout.
func (s *Store) ListTimedOutConcatGroups(modem string, timeout time.Duration) ([]smsconcat.GroupKey, error) {
	cutoff := time.Now().UTC().Add(-timeout).Format(time.RFC3339)
	rows, err := s.db.Query(
		`SELECT from_number, concat_ref, part_total, MIN(created_at) AS first_seen
		 FROM concat_parts
		 WHERE modem = ?
		 GROUP BY from_number, concat_ref, part_total
		 HAVING first_seen <= ?`,
		modem, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []smsconcat.GroupKey
	for rows.Next() {
		var from string
		var ref, total int
		var firstSeen string
		if err := rows.Scan(&from, &ref, &total, &firstSeen); err != nil {
			return nil, err
		}
		parts, err := s.listConcatParts(modem, from, ref, total)
		if err != nil {
			return nil, err
		}
		if len(parts) >= total {
			continue
		}
		keys = append(keys, smsconcat.GroupKey{From: from, ConcatRef: ref, PartTotal: total})
	}
	return keys, rows.Err()
}

// LoadConcatParts loads all parts for a concat group.
func (s *Store) LoadConcatParts(modem string, key smsconcat.GroupKey) ([]ConcatPart, error) {
	return s.listConcatParts(modem, key.From, key.ConcatRef, key.PartTotal)
}

// ConcatPartsToSMS converts stored concat parts to modem.Part values.
func ConcatPartsToSMS(parts []ConcatPart) []modem.Part {
	out := make([]modem.Part, len(parts))
	for i, p := range parts {
		out[i] = modem.Part{
			ID:        p.ModemID,
			From:      p.FromNumber,
			State:     p.State,
			Timestamp: p.ReceivedAt,
			Text:      p.Body,
			ConcatRef: p.ConcatRef,
			Part:      p.PartNum,
			PartTotal: p.PartTotal,
		}
	}
	return out
}

// MergedMessageFromParts builds a modem.Message from concat parts.
func MergedMessageFromParts(parts []ConcatPart, incomplete bool) modem.Message {
	smsParts := ConcatPartsToSMS(parts)
	if incomplete {
		return smsconcat.MergeIncompleteGroup(smsParts)
	}
	return smsconcat.MergeGroup(smsParts)
}
