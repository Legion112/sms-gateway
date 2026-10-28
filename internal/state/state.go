// Package state publishes what the watch daemon knows about its modems so
// another process can report it.
//
// The daemon holds every serial port it watches open for the life of the
// process, so a CLI cannot ask the modem anything while the daemon runs: the
// port is busy, and taking it away would stop reception to answer a question
// about reception. Everything worth reporting is already in the daemon's own
// health records, so it writes them here instead and the CLI reads the file.
//
// This is a diagnostic, not a protocol. The file is rewritten whole on every
// update, it is never read by the daemon, and losing it costs nothing but a
// `status` falling back to the error it used to print.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
)

// Snapshot is one write of the daemon's view.
type Snapshot struct {
	// WrittenAt dates every reading below. A reader must report it: the
	// alternative is presenting an hours-old file as the current state of the
	// radio, which is worse than printing nothing.
	WrittenAt time.Time `json:"written_at"`
	// PID is the daemon that wrote this, for an operator matching it against
	// systemd. It is not a liveness check.
	PID int `json:"pid"`
	// WriteInterval is how often the daemon expects to rewrite this file, so a
	// reader can judge staleness against the daemon's real cadence instead of
	// a constant of its own.
	WriteInterval time.Duration `json:"write_interval_ns"`
	Modems        []Modem       `json:"modems"`
}

// Modem is the daemon's record for one configured modem.
//
// The registration states are stored as the 3GPP numbers they came from rather
// than as text: this file round-trips through a reader that renders them, and a
// second spelling of the same values is a second thing to keep in step.
type Modem struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Device string `json:"device,omitempty"`

	// ICCID and Phone are read once when the modem opens and cannot change
	// without a reconnect, so they are as current as the connection is.
	ICCID string `json:"iccid,omitempty"`
	Phone string `json:"phone,omitempty"`

	Connected  bool      `json:"connected"`
	Stalled    bool      `json:"stalled"`
	LastPollOK time.Time `json:"last_poll_ok,omitempty"`
	Polls      int       `json:"polls"`
	Reconnects int       `json:"reconnects"`

	CS  modem.RegState `json:"cs_registration"`
	PS  modem.RegState `json:"ps_registration"`
	IMS modem.RegState `json:"ims_registration"`
	// SMSPath is empty until the first successful registration read, which is
	// different from a read that produced no verdict.
	SMSPath modem.SMSPath `json:"sms_path,omitempty"`
	// RegCheckedAt dates the three states above. They are polled on their own
	// interval, so they are older than WrittenAt by up to that interval.
	RegCheckedAt time.Time `json:"registration_checked_at,omitempty"`
	// RegUnreadable means the last attempt to read the registration failed, so
	// the states above are the last ones that worked rather than current.
	RegUnreadable bool `json:"registration_unreadable,omitempty"`
}

// Registration rebuilds the per-domain reading for the shared verdict logic, so
// a reader never re-implements what a registration means.
func (m Modem) Registration() modem.Registration {
	return modem.Registration{CS: m.CS, PS: m.PS, IMS: m.IMS}
}

// Age reports how old the snapshot is. A negative age means the writer's clock
// is ahead of the reader's, which is reported as zero rather than as a reading
// from the future.
func (s Snapshot) Age(now time.Time) time.Duration {
	if s.WrittenAt.IsZero() {
		return 0
	}
	if age := now.Sub(s.WrittenAt); age > 0 {
		return age
	}
	return 0
}

// Stale reports whether the snapshot is old enough that the daemon has
// probably stopped writing it. The threshold is three of the daemon's own write
// intervals: one missed write is a busy moment, three is a daemon that is gone.
func (s Snapshot) Stale(now time.Time) bool {
	interval := s.WriteInterval
	if interval <= 0 {
		interval = defaultWriteInterval
	}
	return s.Age(now) > 3*interval
}

// defaultWriteInterval stands in for a snapshot written by a version that did
// not record its cadence.
const defaultWriteInterval = 5 * time.Minute

// Find returns the record for one modem by name.
func (s Snapshot) Find(name string) (Modem, bool) {
	for _, m := range s.Modems {
		if m.Name == name {
			return m, true
		}
	}
	return Modem{}, false
}

// Write replaces the file at path with snap.
//
// The write is atomic: a reader either sees the previous snapshot or this one,
// never half of either. A reader that catches a torn file would report a modem
// state assembled from two different moments, which is the one thing a
// diagnostic must not do.
func Write(path string, snap Snapshot) error {
	if path == "" {
		return fmt.Errorf("state: no path configured")
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("state: encode: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".watch-state-*.tmp")
	if err != nil {
		return fmt.Errorf("state: create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// Every later failure leaves the temp file behind, so the cleanup is armed
	// before the first of them and disarmed only by a successful rename.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("state: write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("state: close %s: %w", tmpName, err)
	}
	// CreateTemp makes the file 0600; this one is meant to be readable by an
	// operator running the CLI as themselves. It carries no secrets.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("state: chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("state: rename onto %s: %w", path, err)
	}
	tmpName = ""
	return nil
}

// Read loads the snapshot at path. A missing file reports an error wrapping
// fs.ErrNotExist, which callers distinguish with errors.Is: no file is the
// normal state of a host where the daemon has never run, not a fault.
func Read(path string) (Snapshot, error) {
	if path == "" {
		return Snapshot{}, fmt.Errorf("state: no path configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("state: read %s: %w", path, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("state: parse %s: %w", path, err)
	}
	return snap, nil
}
