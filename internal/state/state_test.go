package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
)

func sample() Snapshot {
	return Snapshot{
		WrittenAt:     time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC),
		PID:           4242,
		WriteInterval: 150 * time.Second,
		Modems: []Modem{{
			Name:         "ec25-main",
			Driver:       "serial",
			Device:       "/dev/ttyUSB6",
			ICCID:        "8970199",
			Phone:        "+70000000000",
			Connected:    true,
			LastPollOK:   time.Date(2026, 10, 28, 10, 59, 51, 0, time.UTC),
			Polls:        1620,
			CS:           modem.RegRoaming,
			PS:           modem.RegRoaming,
			IMS:          modem.RegAbsent,
			SMSPath:      modem.SMSPathDirect,
			RegCheckedAt: time.Date(2026, 10, 28, 10, 58, 0, 0, time.UTC),
		}},
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch-state.json")
	want := sample()
	if err := Write(path, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.PID != want.PID || got.WriteInterval != want.WriteInterval {
		t.Errorf("header lost: got pid=%d interval=%s", got.PID, got.WriteInterval)
	}
	if !got.WrittenAt.Equal(want.WrittenAt) {
		t.Errorf("WrittenAt = %v, want %v", got.WrittenAt, want.WrittenAt)
	}
	if len(got.Modems) != 1 {
		t.Fatalf("got %d modems, want 1", len(got.Modems))
	}
	g, w := got.Modems[0], want.Modems[0]
	if g.Name != w.Name || g.Device != w.Device || g.Polls != w.Polls || g.Connected != w.Connected {
		t.Errorf("modem fields lost: %+v", g)
	}
	if g.CS != w.CS || g.PS != w.PS || g.IMS != w.IMS || g.SMSPath != w.SMSPath {
		t.Errorf("registration lost: cs=%v ps=%v ims=%v path=%v", g.CS, g.PS, g.IMS, g.SMSPath)
	}
}

// RegAbsent is -1 and RegNotRegistered is 0. A format that dropped the
// distinction would turn "the modem never answered" into "the modem said it is
// not registered", which is the one confusion this whole feature exists to
// avoid.
func TestAbsentSurvivesTheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	snap := Snapshot{Modems: []Modem{{
		Name: "m", CS: modem.RegAbsent, PS: modem.RegNotRegistered, IMS: modem.RegAbsent,
	}}}
	if err := Write(path, snap); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	m := got.Modems[0]
	if m.CS != modem.RegAbsent {
		t.Errorf("CS = %v, want RegAbsent", m.CS)
	}
	if m.PS != modem.RegNotRegistered {
		t.Errorf("PS = %v, want RegNotRegistered", m.PS)
	}
	if m.CS == m.PS {
		t.Error("absent and not-registered came back identical")
	}
}

func TestRegistrationRebuildsTheVerdict(t *testing.T) {
	m := Modem{CS: modem.RegNotRegistered, PS: modem.RegRoaming, IMS: modem.RegNotRegistered}
	if got := m.Registration().SMSPath(); got != modem.SMSPathUnproven {
		t.Errorf("SMSPath() = %q, want unproven", got)
	}
}

func TestWriteReplacesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	first := Snapshot{PID: 1, Modems: []Modem{{Name: "a"}, {Name: "b"}}}
	if err := Write(path, first); err != nil {
		t.Fatalf("Write: %v", err)
	}
	second := Snapshot{PID: 2, Modems: []Modem{{Name: "a"}}}
	if err := Write(path, second); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.PID != 2 || len(got.Modems) != 1 {
		t.Errorf("the file was not replaced: pid=%d modems=%d", got.PID, len(got.Modems))
	}
}

// The write is atomic, so it must not leave its temporary files behind.
func TestWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watch-state.json")
	for range 5 {
		if err := Write(path, sample()); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "watch-state.json" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only watch-state.json", names)
	}
}

func TestWriteIsReadableByTheOperator(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := Write(path, sample()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// The daemon runs as root and the CLI does not. A 0600 file would make the
	// fallback work only for root, which is the case it is least needed in.
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("mode = %o, want 644", perm)
	}
}

func TestWriteRejectsAnEmptyPath(t *testing.T) {
	if err := Write("", sample()); err == nil {
		t.Error("Write accepted an empty path")
	}
}

// A host where the daemon has never run has no file, and the caller has to be
// able to tell that from a corrupt one.
func TestReadOfMissingFileIsNotExist(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), "absent.json"))
	if err == nil {
		t.Fatal("Read of a missing file succeeded")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error does not wrap fs.ErrNotExist: %v", err)
	}
}

func TestReadOfGarbageFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Error("Read accepted a corrupt file")
	}
}

func TestReadRejectsAnEmptyPath(t *testing.T) {
	if _, err := Read(""); err == nil {
		t.Error("Read accepted an empty path")
	}
}

func TestWriteFailsWhenTheDirectoryIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "s.json")
	if err := Write(path, sample()); err == nil {
		t.Error("Write into a missing directory succeeded")
	}
}

func TestAge(t *testing.T) {
	at := time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC)
	snap := Snapshot{WrittenAt: at}
	if got := snap.Age(at.Add(90 * time.Second)); got != 90*time.Second {
		t.Errorf("Age = %s, want 1m30s", got)
	}
	// Clock skew must not be reported as a reading from the future.
	if got := snap.Age(at.Add(-time.Hour)); got != 0 {
		t.Errorf("Age with a skewed clock = %s, want 0", got)
	}
	if got := (Snapshot{}).Age(at); got != 0 {
		t.Errorf("Age of an undated snapshot = %s, want 0", got)
	}
}

func TestStaleFollowsTheWritersCadence(t *testing.T) {
	at := time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC)
	snap := Snapshot{WrittenAt: at, WriteInterval: time.Minute}
	for _, tc := range []struct {
		elapsed time.Duration
		want    bool
	}{
		{30 * time.Second, false},
		{2 * time.Minute, false},
		{3 * time.Minute, false},
		{3*time.Minute + time.Second, true},
		{time.Hour, true},
	} {
		if got := snap.Stale(at.Add(tc.elapsed)); got != tc.want {
			t.Errorf("Stale after %s = %t, want %t", tc.elapsed, got, tc.want)
		}
	}
}

// A snapshot from a build that did not record its cadence still has to be
// judged, rather than counted fresh forever.
func TestStaleWithoutACadenceUsesTheDefault(t *testing.T) {
	at := time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC)
	snap := Snapshot{WrittenAt: at}
	if snap.Stale(at.Add(10 * time.Minute)) {
		t.Error("a 10m-old snapshot was called stale under the 5m default")
	}
	if !snap.Stale(at.Add(time.Hour)) {
		t.Error("an hour-old snapshot was not called stale")
	}
}

func TestFind(t *testing.T) {
	snap := sample()
	if _, ok := snap.Find("ec25-main"); !ok {
		t.Error("Find missed a modem that is present")
	}
	if _, ok := snap.Find("ec25-backup"); ok {
		t.Error("Find invented a modem that is absent")
	}
}
