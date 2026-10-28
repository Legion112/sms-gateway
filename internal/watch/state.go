package watch

import (
	"fmt"
	"os"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/state"
)

// publishState writes what the daemon knows about its modems to the state file.
//
// It is called on every transition worth seeing promptly -- a modem opening or
// closing, a change in the SMS path -- and on the health supervisor's tick, so
// the file also proves the daemon is still running rather than only recording
// the last thing that happened to it.
//
// Nothing here can fail the caller. A diagnostic that cannot be written is a
// lost diagnostic, not a lost SMS, so the error is logged on the edge and the
// daemon carries on.
func (s *Service) publishState() {
	path := s.cfg.StatePath()
	if path == "" {
		return
	}
	// Built before the lock: the snapshot reads the health records under their
	// own mutex, and nesting the two locks in one order here and the other
	// order anywhere else would be a deadlock waiting for a coincidence.
	snap := s.stateSnapshot()

	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	err := state.Write(path, snap)
	failed := err != nil
	if failed == s.stateWriteFailed {
		return
	}
	s.stateWriteFailed = failed
	if failed {
		s.deps.Log("state_unpublished path=%s error=%v", path, redactSecrets(err))
		return
	}
	s.deps.Log("state_published path=%s (writable again)", path)
}

// stateSnapshot assembles the current view. It only reads records the daemon
// already keeps: nothing here asks a modem anything, because the whole reason
// this file exists is that the ports are busy.
func (s *Service) stateSnapshot() state.Snapshot {
	names := s.modemNames()
	modems := make([]state.Modem, 0, len(names))
	for _, name := range names {
		h := s.healthSnapshot(name)
		line := s.simLineFor(name)
		entry := s.cfg.Modems[name]
		modems = append(modems, state.Modem{
			Name:          name,
			Driver:        entry.Driver,
			Device:        modemDevice(entry),
			ICCID:         line.ICCID,
			Phone:         line.Number,
			Connected:     h.connected,
			Stalled:       h.stalled,
			LastPollOK:    h.lastPollOK,
			Polls:         h.polls,
			Reconnects:    h.reconnects,
			CS:            h.registration.CS,
			PS:            h.registration.PS,
			IMS:           h.registration.IMS,
			SMSPath:       h.smsPath,
			RegCheckedAt:  h.lastRegCheck,
			RegUnreadable: h.regReadFailed,
		})
	}
	return state.Snapshot{
		WrittenAt:     s.now(),
		PID:           os.Getpid(),
		WriteInterval: time.Duration(s.stateInterval.Load()),
		Modems:        modems,
	}
}

// modemDevice names the port or object a modem entry points at, for an operator
// matching a report against the hardware.
func modemDevice(entry config.ModemEntry) string {
	switch entry.Driver {
	case config.DriverSerial:
		return entry.Serial.Device
	case config.DriverMM:
		if entry.MM.ModemPath != "" {
			return entry.MM.ModemPath
		}
		return fmt.Sprintf("modem %d", entry.MM.ModemIndex)
	}
	return ""
}
