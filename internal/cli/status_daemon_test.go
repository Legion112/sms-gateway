package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/state"
)

// capture collects what the status printers write, which is fmt.Printf like the
// rest of this package.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = saved
	return <-done
}

// busyPort is the error a CLI gets from a port the daemon is holding, which is
// the whole reason the fallback exists.
var busyPort = errors.New("open /dev/ttyUSB6: Serial port busy")

type daemonFixture struct {
	cfg  config.Config
	path string
}

func newDaemonFixture(t *testing.T, modems ...state.Modem) daemonFixture {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{Modems: map[string]config.ModemEntry{}}
	cfg.Storage.Path = filepath.Join(dir, "sms.db")
	for _, m := range modems {
		cfg.Modems[m.Name] = config.ModemEntry{Driver: config.DriverSerial}
	}
	snap := state.Snapshot{
		WrittenAt:     time.Now(),
		PID:           4242,
		WriteInterval: 2 * time.Minute,
		Modems:        modems,
	}
	if err := state.Write(cfg.StatePath(), snap); err != nil {
		t.Fatal(err)
	}
	return daemonFixture{cfg: cfg, path: cfg.StatePath()}
}

func healthyRecord(name string) state.Modem {
	return state.Modem{
		Name: name, Driver: config.DriverSerial, Device: "/dev/ttyUSB6",
		Connected: true, LastPollOK: time.Now().Add(-9 * time.Second), Polls: 1620,
		CS: modem.RegRoaming, PS: modem.RegRoaming, IMS: modem.RegAbsent,
		SMSPath: modem.SMSPathDirect, RegCheckedAt: time.Now().Add(-70 * time.Second),
	}
}

// The fix: a busy port no longer costs the operator the whole report.
func TestBusyPortFallsBackToTheDaemon(t *testing.T) {
	f := newDaemonFixture(t, healthyRecord("ec25-main"))

	var ok bool
	out := capture(t, func() {
		ok = reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort)
	})
	if !ok {
		t.Fatalf("the fallback declined; output=%q", out)
	}
	for _, want := range []string{
		"source: watch daemon (pid 4242)",
		"driver: serial",
		"device: /dev/ttyUSB6",
		"state: up",
		"polls: 1620",
		"cs_registration: registered (roaming)",
		"ps_registration: registered (roaming)",
		"ims_registration: unsupported",
		"sms_path: direct",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

// An operator who expected to talk to the modem must be told why the numbers
// come from somewhere else.
func TestFallbackNamesItsSourceAndReason(t *testing.T) {
	f := newDaemonFixture(t, healthyRecord("m1"))

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "Serial port busy") {
		t.Errorf("the original error was swallowed:\n%s", out)
	}
	if !strings.Contains(out, "published") {
		t.Errorf("the reading is undated:\n%s", out)
	}
	if !strings.Contains(out, "note: sim state, message count and live network state") {
		t.Errorf("the output does not say what it could not report:\n%s", out)
	}
}

// The fault this feature exists for has to carry its warning through the
// fallback, or the one path an operator can actually use says nothing.
func TestUnprovenPathWarnsThroughTheFallback(t *testing.T) {
	rec := healthyRecord("m1")
	rec.CS = modem.RegNotRegistered
	rec.IMS = modem.RegNotRegistered
	rec.SMSPath = modem.SMSPathUnproven
	f := newDaemonFixture(t, rec)

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "sms_path: unproven") {
		t.Errorf("the verdict is missing:\n%s", out)
	}
	if !strings.Contains(out, "warning:") || !strings.Contains(out, "over SGs") {
		t.Errorf("the warning is missing:\n%s", out)
	}
}

func TestOfflinePathWarnsThroughTheFallback(t *testing.T) {
	rec := healthyRecord("m1")
	rec.CS, rec.PS, rec.IMS = modem.RegNotRegistered, modem.RegNotRegistered, modem.RegNotRegistered
	rec.SMSPath = modem.SMSPathOffline
	f := newDaemonFixture(t, rec)

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "cannot receive SMS") {
		t.Errorf("an offline modem produced no warning:\n%s", out)
	}
}

// A modem the daemon has not read yet must not be reported as unregistered.
func TestUnreadRegistrationSaysSo(t *testing.T) {
	rec := state.Modem{
		Name: "m1", Driver: config.DriverSerial, Connected: true,
		CS: modem.RegAbsent, PS: modem.RegAbsent, IMS: modem.RegAbsent,
	}
	f := newDaemonFixture(t, rec)

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "sms_path: not read yet") {
		t.Errorf("an unread modem did not say so:\n%s", out)
	}
	if strings.Contains(out, "cs_registration:") {
		t.Errorf("an unread modem printed domain states:\n%s", out)
	}
	if strings.Contains(out, "warning:") {
		t.Errorf("an unread modem produced a warning:\n%s", out)
	}
}

// A failed read leaves the last good states in place, so the report has to say
// they are not current.
func TestUnreadableRegistrationIsFlagged(t *testing.T) {
	rec := healthyRecord("m1")
	rec.RegUnreadable = true
	f := newDaemonFixture(t, rec)

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "the last registration read failed") {
		t.Errorf("a failed read was not flagged:\n%s", out)
	}
}

func TestDisconnectedModemIsReported(t *testing.T) {
	rec := healthyRecord("m1")
	rec.Connected = false
	rec.Reconnects = 3
	f := newDaemonFixture(t, rec)

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "state: down") {
		t.Errorf("a disconnected modem was not reported as down:\n%s", out)
	}
	if !strings.Contains(out, "reconnects: 3") {
		t.Errorf("the reconnect count is missing:\n%s", out)
	}
}

func TestStalledModemIsFlagged(t *testing.T) {
	rec := healthyRecord("m1")
	rec.Stalled = true
	f := newDaemonFixture(t, rec)

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "connected but not polling") {
		t.Errorf("a stalled modem was not flagged:\n%s", out)
	}
}

// A file left behind by a daemon that has stopped must not be presented as the
// current state of the radio.
func TestStaleStateIsCalledOut(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	cfg.Storage.Path = filepath.Join(dir, "sms.db")
	snap := state.Snapshot{
		WrittenAt:     time.Now().Add(-4 * time.Hour),
		PID:           11,
		WriteInterval: 2 * time.Minute,
		Modems:        []state.Modem{healthyRecord("m1")},
	}
	if err := state.Write(cfg.StatePath(), snap); err != nil {
		t.Fatal(err)
	}

	out := capture(t, func() { reportStatusFromDaemon(cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "the daemon may have stopped") {
		t.Errorf("a four-hour-old snapshot was presented as current:\n%s", out)
	}
}

func TestFreshStateIsNotCalledStale(t *testing.T) {
	f := newDaemonFixture(t, healthyRecord("m1"))
	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if strings.Contains(out, "may have stopped") {
		t.Errorf("a fresh snapshot was called stale:\n%s", out)
	}
}

// Declining is what makes the fallback safe: the caller then prints the real
// error instead of a half-answer.
func TestFallbackDeclinesWithoutAStateFile(t *testing.T) {
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	cfg.Storage.Path = filepath.Join(t.TempDir(), "sms.db")

	var ok bool
	out := capture(t, func() { ok = reportStatusFromDaemon(cfg, cmdutil.Flags{}, busyPort) })
	if ok {
		t.Errorf("the fallback claimed an answer with no state file:\n%s", out)
	}
	if out != "" {
		t.Errorf("a declining fallback printed %q", out)
	}
}

func TestFallbackDeclinesWithoutStorageConfigured(t *testing.T) {
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	if reportStatusFromDaemon(cfg, cmdutil.Flags{}, busyPort) {
		t.Error("the fallback claimed an answer with no state path")
	}
}

func TestFallbackDeclinesOnACorruptFile(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Modems: map[string]config.ModemEntry{"m1": {Driver: config.DriverSerial}}}
	cfg.Storage.Path = filepath.Join(dir, "sms.db")
	if err := os.WriteFile(cfg.StatePath(), []byte("{ truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if reportStatusFromDaemon(cfg, cmdutil.Flags{}, busyPort) {
		t.Error("the fallback read a corrupt file as an answer")
	}
}

// Two modems configured, one published: reporting only the one present would
// read as a report on both.
func TestFallbackDeclinesWhenAModemIsMissing(t *testing.T) {
	f := newDaemonFixture(t, healthyRecord("m1"))
	f.cfg.Modems["m2"] = config.ModemEntry{Driver: config.DriverSerial}

	var ok bool
	out := capture(t, func() { ok = reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if ok {
		t.Errorf("the fallback answered for only one of two modems:\n%s", out)
	}
}

// With --modem, only that one has to be published, and only that one reported.
func TestFallbackHonoursTheModemFlag(t *testing.T) {
	first := healthyRecord("m1")
	first.Device = "/dev/ttyUSB0"
	second := healthyRecord("m2")
	second.Device = "/dev/ttyUSB9"
	f := newDaemonFixture(t, first, second)

	var ok bool
	out := capture(t, func() {
		ok = reportStatusFromDaemon(f.cfg, cmdutil.Flags{Modem: "m2"}, busyPort)
	})
	if !ok {
		t.Fatalf("the fallback declined a published modem:\n%s", out)
	}
	if !strings.Contains(out, "/dev/ttyUSB9") {
		t.Errorf("--modem m2 did not report m2:\n%s", out)
	}
	if strings.Contains(out, "/dev/ttyUSB0") {
		t.Errorf("--modem m2 reported m1 as well:\n%s", out)
	}
	if n := strings.Count(out, "sms_path:"); n != 1 {
		t.Errorf("printed %d sms_path lines, want 1:\n%s", n, out)
	}
}

func TestFallbackDeclinesForAnUnknownModemFlag(t *testing.T) {
	f := newDaemonFixture(t, healthyRecord("m1"))
	if reportStatusFromDaemon(f.cfg, cmdutil.Flags{Modem: "nope"}, busyPort) {
		t.Error("the fallback answered for a modem that is not configured")
	}
}

// Both modems in one report, each under its own heading.
func TestFallbackReportsEveryModem(t *testing.T) {
	f := newDaemonFixture(t, healthyRecord("ec25-main"), healthyRecord("ec25-backup"))

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	for _, name := range []string{"ec25-main", "ec25-backup"} {
		if !strings.Contains(out, name) {
			t.Errorf("%s is missing from the report:\n%s", name, out)
		}
	}
	if n := strings.Count(out, "sms_path:"); n != 2 {
		t.Errorf("printed %d sms_path lines, want 2:\n%s", n, out)
	}
}

// The phone number comes from sim_numbers at read time, so a config edit shows
// up without waiting for the daemon to republish.
func TestPhoneIsResolvedFromConfig(t *testing.T) {
	rec := healthyRecord("m1")
	rec.ICCID = "8970199000000000001"
	f := newDaemonFixture(t, rec)
	f.cfg.SimNumbers = map[string]string{"8970199000000000001": "+70000000001"}

	out := capture(t, func() { reportStatusFromDaemon(f.cfg, cmdutil.Flags{}, busyPort) })
	if !strings.Contains(out, "+70000000001") {
		t.Errorf("the resolved line is missing:\n%s", out)
	}
}

func TestAgoFormatting(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{9 * time.Second, "9s"},
		{90 * time.Second, "1m30s"},
		{500 * time.Millisecond, "less than a second"},
		{0, "less than a second"},
		{-time.Hour, "less than a second"},
	} {
		if got := ago(tc.in); got != tc.want {
			t.Errorf("ago(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
