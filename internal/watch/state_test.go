package watch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/state"
)

// statePath points a fixture's publishing at a temp file and returns the path.
func statePath(t *testing.T, f *livenessFixture) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "watch-state.json")
	f.svc.cfg.Watch.StatePath = path
	return path
}

func readState(t *testing.T, path string) state.Snapshot {
	t.Helper()
	snap, err := state.Read(path)
	if err != nil {
		t.Fatalf("reading the published state: %v", err)
	}
	return snap
}

// The point of the file: a running daemon publishes what a CLI cannot read,
// because the daemon is holding the port.
func TestStateIsPublishedForARunningModem(t *testing.T) {
	m := newRegistrationModem(directReg(), hello())
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	path := statePath(t, f)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool {
		snap, err := state.Read(path)
		if err != nil || len(snap.Modems) == 0 {
			return false
		}
		return snap.Modems[0].SMSPath == modem.SMSPathDirect
	}, 5*time.Second) {
		t.Fatalf("the SMS path never reached the state file; log=%v", f.log.all())
	}

	snap := readState(t, path)
	rec := snap.Modems[0]
	if rec.Name != "m1" {
		t.Errorf("name = %q, want m1", rec.Name)
	}
	if rec.Driver != config.DriverSerial {
		t.Errorf("driver = %q, want serial", rec.Driver)
	}
	if !rec.Connected {
		t.Error("a watched modem was published as disconnected")
	}
	if rec.CS != modem.RegRoaming || rec.PS != modem.RegRoaming {
		t.Errorf("registration not published: cs=%v ps=%v", rec.CS, rec.PS)
	}
	if got := rec.Registration().SMSPath(); got != modem.SMSPathDirect {
		t.Errorf("the published registration yields %q, want direct", got)
	}
	if snap.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", snap.PID, os.Getpid())
	}
	if snap.WrittenAt.IsZero() {
		t.Error("the snapshot is undated, so a reader cannot judge its age")
	}
}

// The fault the SMS-path check exists for has to survive into the file, or
// `status` would report a healthy modem while the journal says otherwise.
func TestUnprovenPathReachesTheStateFile(t *testing.T) {
	m := newRegistrationModem(unprovenReg())
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	path := statePath(t, f)
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool {
		snap, err := state.Read(path)
		return err == nil && len(snap.Modems) == 1 && snap.Modems[0].SMSPath == modem.SMSPathUnproven
	}, 5*time.Second) {
		t.Fatalf("the unproven path never reached the state file; log=%v", f.log.all())
	}
}

// A modem whose registration has never been read must not be published as
// "not registered": RegState 0 is a real answer, and the record starts at
// RegAbsent precisely so the two cannot be confused.
func TestNeverReadRegistrationIsPublishedAsAbsent(t *testing.T) {
	f := newLivenessFixture(t, serialModems("m1", "m2"), nil, newTestClock(32))
	path := statePath(t, f)
	defer f.close()

	f.svc.publishState()
	snap := readState(t, path)
	if len(snap.Modems) != 2 {
		t.Fatalf("published %d modems, want 2", len(snap.Modems))
	}
	for _, rec := range snap.Modems {
		if rec.SMSPath != "" {
			t.Errorf("%s: sms_path = %q, want empty before the first read", rec.Name, rec.SMSPath)
		}
		if rec.CS != modem.RegAbsent || rec.PS != modem.RegAbsent || rec.IMS != modem.RegAbsent {
			t.Errorf("%s: an unread registration published as cs=%v ps=%v ims=%v, want all absent",
				rec.Name, rec.CS, rec.PS, rec.IMS)
		}
	}
}

// Every configured modem belongs in the file whether or not it is up, or
// `status` could not report the one that is down -- which is when it is asked.
func TestEveryConfiguredModemIsPublished(t *testing.T) {
	f := newLivenessFixture(t, serialModems("a", "b", "c"), nil, newTestClock(32))
	path := statePath(t, f)
	defer f.close()

	f.svc.publishState()
	snap := readState(t, path)
	for _, name := range []string{"a", "b", "c"} {
		rec, ok := snap.Find(name)
		if !ok {
			t.Fatalf("modem %s is missing from the state file", name)
		}
		if rec.Connected {
			t.Errorf("%s: published as connected before it was opened", name)
		}
	}
}

// A disconnect has to show up promptly: this is the state an operator runs
// `status` to understand.
func TestDisconnectIsPublished(t *testing.T) {
	f := newLivenessFixture(t, serialModems("m1"), nil, newTestClock(32))
	path := statePath(t, f)
	defer f.close()

	f.svc.markModemUp("m1")
	if rec, _ := readState(t, path).Find("m1"); !rec.Connected {
		t.Fatal("markModemUp did not publish a connected modem")
	}
	f.svc.markModemDown("m1", errors.New("port vanished"))
	rec, _ := readState(t, path).Find("m1")
	if rec.Connected {
		t.Error("markModemDown did not publish the disconnect")
	}
	if rec.Reconnects != 1 {
		t.Errorf("reconnects = %d, want 1", rec.Reconnects)
	}
}

// The device is what an operator matches the report against the hardware with.
func TestPublishedDeviceNamesThePort(t *testing.T) {
	modems := map[string]config.ModemEntry{
		"ser": {Driver: config.DriverSerial, Serial: config.SerialConfig{Device: "/dev/ttyUSB6"}},
		"mm":  {Driver: config.DriverMM, MM: config.MMConfig{ModemIndex: 3}},
	}
	f := newLivenessFixture(t, modems, nil, newTestClock(32))
	path := statePath(t, f)
	defer f.close()

	f.svc.publishState()
	snap := readState(t, path)
	if rec, _ := snap.Find("ser"); rec.Device != "/dev/ttyUSB6" {
		t.Errorf("serial device = %q, want /dev/ttyUSB6", rec.Device)
	}
	if rec, _ := snap.Find("mm"); rec.Device != "modem 3" {
		t.Errorf("mm device = %q, want \"modem 3\"", rec.Device)
	}
}

func TestPublishedDeviceForMMPath(t *testing.T) {
	modems := map[string]config.ModemEntry{
		"mm": {Driver: config.DriverMM, MM: config.MMConfig{ModemPath: "/org/freedesktop/ModemManager1/Modem/7"}},
	}
	f := newLivenessFixture(t, modems, nil, newTestClock(32))
	path := statePath(t, f)
	defer f.close()

	f.svc.publishState()
	if rec, _ := readState(t, path).Find("mm"); rec.Device != "/org/freedesktop/ModemManager1/Modem/7" {
		t.Errorf("mm device = %q, want the object path", rec.Device)
	}
}

// No configured path means no publishing, and in particular no error: a build
// without storage still receives SMS.
func TestPublishingIsSkippedWithoutAPath(t *testing.T) {
	f := newLivenessFixture(t, serialModems("m1"), nil, newTestClock(32))
	defer f.close()
	f.svc.cfg.Watch.StatePath = ""
	f.svc.cfg.Storage.Path = ""

	f.svc.publishState()
	if lines := f.log.all(); len(lines) != 0 {
		t.Errorf("publishing without a path logged %v, want silence", lines)
	}
}

// A file that cannot be written is a lost diagnostic, not a lost SMS. It is
// reported once rather than on every transition.
func TestUnwritableStateIsLoggedOnceAndHarmless(t *testing.T) {
	f := newLivenessFixture(t, serialModems("m1"), nil, newTestClock(32))
	defer f.close()
	f.svc.cfg.Watch.StatePath = filepath.Join(t.TempDir(), "no-such-dir", "watch-state.json")

	for range 4 {
		f.svc.publishState()
	}
	if n := countLines(f.log.all(), "state_unpublished"); n != 1 {
		t.Errorf("an unwritable state file was reported %d times, want 1: %v", n, f.log.all())
	}
}

// And when it becomes writable again, that is worth one line too, so an
// operator who fixed it knows it took.
func TestStateRecoveryIsReported(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone")
	f := newLivenessFixture(t, serialModems("m1"), nil, newTestClock(32))
	defer f.close()
	f.svc.cfg.Watch.StatePath = filepath.Join(missing, "watch-state.json")

	f.svc.publishState()
	if n := countLines(f.log.all(), "state_unpublished"); n != 1 {
		t.Fatalf("the failure was not reported: %v", f.log.all())
	}
	if err := os.Mkdir(missing, 0o755); err != nil {
		t.Fatal(err)
	}
	f.svc.publishState()
	if n := countLines(f.log.all(), "state_published"); n != 1 {
		t.Errorf("the recovery was not reported once: %v", f.log.all())
	}
	f.svc.publishState()
	if n := countLines(f.log.all(), "state_published"); n != 1 {
		t.Errorf("the recovery was reported again on an unchanged write: %v", f.log.all())
	}
}

// The file records the daemon's own publishing cadence, so a reader judges
// staleness against what the daemon actually does rather than a constant of
// its own that could drift out of step.
func TestPublishedCadenceIsRecorded(t *testing.T) {
	m := newRegistrationModem(directReg())
	opener := &scriptedOpener{script: []func() (modem.Modem, error){opensRegistrationModem(m)}}

	f := newLivenessFixture(t, serialModems("m1"), opener.open, newTestClock(32))
	path := statePath(t, f)
	f.svc.cfg.Watch.StallTimeout = 4 * time.Minute
	f.svc.cfg.Watch.HealthInterval = time.Hour
	defer f.close()
	_, stop := runService(t, f.svc)
	defer stop()

	if !waitFor(func() bool {
		snap, err := state.Read(path)
		return err == nil && snap.WriteInterval > 0
	}, 5*time.Second) {
		t.Fatalf("the publishing cadence was never recorded; log=%v", f.log.all())
	}
	// The health supervisor ticks at half the stall timeout, which is what
	// refreshes the file.
	if got := readState(t, path).WriteInterval; got != 2*time.Minute {
		t.Errorf("WriteInterval = %s, want 2m (half of a 4m stall timeout)", got)
	}
}

// A snapshot taken while the daemon is writing must never be half of each: the
// reader would report a modem assembled from two different moments.
func TestConcurrentPublishingStaysReadable(t *testing.T) {
	f := newLivenessFixture(t, serialModems("a", "b"), nil, newTestClock(32))
	path := statePath(t, f)
	defer f.close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			f.svc.publishState()
		}
	}()
	for range 200 {
		if _, err := state.Read(path); err != nil && !os.IsNotExist(errors.Unwrap(err)) {
			t.Errorf("read a torn state file: %v", err)
			break
		}
	}
	<-done
}

func TestStatePathDefaultsBesideTheDatabase(t *testing.T) {
	cfg := config.Config{}
	cfg.Storage.Path = "/var/lib/sms-gateway/sms.db"
	if got := cfg.StatePath(); got != "/var/lib/sms-gateway/watch-state.json" {
		t.Errorf("StatePath() = %q, want it beside the database", got)
	}
}

func TestStatePathOverrideWins(t *testing.T) {
	cfg := config.Config{}
	cfg.Storage.Path = "/var/lib/sms-gateway/sms.db"
	cfg.Watch.StatePath = "/run/sms-gateway/state.json"
	if got := cfg.StatePath(); got != "/run/sms-gateway/state.json" {
		t.Errorf("StatePath() = %q, want the configured override", got)
	}
}

func TestStatePathEmptyWithoutStorage(t *testing.T) {
	if got := (config.Config{}).StatePath(); got != "" {
		t.Errorf("StatePath() = %q, want empty with no storage configured", got)
	}
}
