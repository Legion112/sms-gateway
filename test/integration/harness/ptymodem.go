//go:build integration && linux

// Package harness runs the gateway against a modem that is real as far as the
// driver can tell: a pseudo-terminal speaking the AT protocol, opened through a
// device node that can be made to disappear and come back.
//
// The unit tests in internal/watch prove the supervisor reconnects when a fake
// modem returns an error. They cannot prove it reconnects when the device node
// itself vanishes and is recreated under a different pts number, which is what
// a USB re-enumeration actually does and is the failure that took a SIM out of
// service in production.
package harness

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// SerialModem is a fake Quectel EC25 reachable over a pty.
//
// Link is a stable path the driver is pointed at, standing in for the udev node.
// Unplug removes it and drops the pty; Replug creates a new pty behind the same
// path, exactly as the kernel would hand out a different /dev/ttyUSB* after a
// re-enumeration.
type SerialModem struct {
	mu       sync.Mutex
	link     string
	master   *os.File
	session  int
	messages map[int]string // CMGL index -> PDU hex
	deleted  []int
	commands []string
	closed   bool

	// scanMode is AT+QCFG="nwscanmode": 0 automatic, 1 GSM, 2 WCDMA, 3 LTE.
	// copsMode is AT+COPS <mode>: 0 automatic, 1 manual. Both survive a Replug,
	// as a real modem's NV does across a USB re-enumeration.
	scanMode int
	copsMode int

	// creg, cgreg and cereg are the <stat> each registration query reports,
	// and ims the <reg_info> of +CIREG. They start where a modem on 2G/3G at
	// home does: circuit-switched and GPRS attached, no LTE, no IMS.
	creg  int
	cgreg int
	cereg int
	ims   int
	// imsSupported false makes +CIREG? answer ERROR, as firmware without the
	// command does.
	imsSupported bool
}

var (
	cmgdRe     = regexp.MustCompile(`^AT\+CMGD=(\d+)`)
	scanReadRe = regexp.MustCompile(`^AT\+QCFG="nwscanmode"$`)
	scanSetRe  = regexp.MustCompile(`^AT\+QCFG="nwscanmode",(\d+)(?:,(\d+))?$`)
	copsSetRe  = regexp.MustCompile(`^AT\+COPS=(\d+)`)
)

// Registration <stat> values, as 3GPP numbers them.
const (
	RegNotRegistered = 0
	RegHome          = 1
	RegSearching     = 2
	RegDenied        = 3
	RegRoaming       = 5
)

// NewSerialModem creates the pty and the device node, and starts answering AT.
func NewSerialModem(link string) (*SerialModem, error) {
	m := &SerialModem{
		link:         link,
		messages:     map[int]string{},
		creg:         RegHome,
		cgreg:        RegHome,
		cereg:        RegNotRegistered,
		imsSupported: true,
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return nil, err
	}
	if err := m.Replug(); err != nil {
		return nil, err
	}
	return m, nil
}

// openPTY returns the master and the /dev/pts/N path of a fresh pair.
func openPTY() (*os.File, string, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open /dev/ptmx: %w", err)
	}
	fd := int(master.Fd())
	// Unlock the slave before asking for its number; the kernel refuses to open
	// a locked slave and the failure looks like a missing device.
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, "", fmt.Errorf("unlock pty: %w", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, "", fmt.Errorf("pty number: %w", err)
	}
	return master, fmt.Sprintf("/dev/pts/%d", n), nil
}

// Replug points the device node at a brand new pty. The pts number differs from
// the previous one, so a driver that cached anything about the old device fails
// here rather than silently appearing to work.
func (m *SerialModem) Replug() error {
	master, slave, err := openPTY()
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = master.Close()
		return fmt.Errorf("modem is closed")
	}
	old := m.master
	m.master = master
	m.session++
	session := m.session
	m.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	_ = os.Remove(m.link)
	if err := os.Symlink(slave, m.link); err != nil {
		return fmt.Errorf("link %s -> %s: %w", m.link, slave, err)
	}

	go m.serve(master, session)
	return nil
}

// Unplug drops the pty and removes the device node, so an open of the link
// fails with ENOENT just as it does while a USB modem is gone.
func (m *SerialModem) Unplug() {
	m.mu.Lock()
	master := m.master
	m.master = nil
	m.session++
	m.mu.Unlock()

	if master != nil {
		_ = master.Close()
	}
	_ = os.Remove(m.link)
}

// Close releases everything; safe to call twice.
func (m *SerialModem) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	master := m.master
	m.master = nil
	m.session++
	m.mu.Unlock()

	if master != nil {
		_ = master.Close()
	}
	_ = os.Remove(m.link)
}

// AddMessage stores one PDU in the given CMGL slot.
func (m *SerialModem) AddMessage(index int, pduHex string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[index] = pduHex
}

// Deleted returns the slots AT+CMGD freed, in order.
func (m *SerialModem) Deleted() []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]int(nil), m.deleted...)
}

// Stored reports how many slots still hold a message.
func (m *SerialModem) Stored() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

// SetRadio puts the modem in a given posture, standing in for a module that
// was left locked by a previous owner, a firmware default or an AT session.
func (m *SerialModem) SetRadio(scanMode, copsMode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scanMode = scanMode
	m.copsMode = copsMode
}

// Radio reports the posture the modem currently holds.
func (m *SerialModem) Radio() (scanMode, copsMode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scanMode, m.copsMode
}

// SetRegistration sets what each domain reports. The circuit-switched and
// packet-switched stats are the 3GPP <stat> values; ims is the +CIREG
// <reg_info>, 1 for an IMS registration.
func (m *SerialModem) SetRegistration(creg, cgreg, cereg, ims int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creg = creg
	m.cgreg = cgreg
	m.cereg = cereg
	m.ims = ims
}

// SetIMSSupported controls whether +CIREG? is answered at all, standing in for
// firmware that does not implement it.
func (m *SerialModem) SetIMSSupported(supported bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.imsSupported = supported
}

// Commands returns every AT command the modem has been sent.
func (m *SerialModem) Commands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.commands...)
}

// serve answers AT commands until its pty is closed or superseded.
func (m *SerialModem) serve(master *os.File, session int) {
	defer master.Close()

	buf := make([]byte, 0, 256)
	chunk := make([]byte, 256)
	for {
		n, err := master.Read(chunk)
		if err != nil {
			return
		}
		m.mu.Lock()
		stale := m.session != session
		m.mu.Unlock()
		if stale {
			return
		}

		buf = append(buf, chunk[:n]...)
		for {
			i := bytes.IndexAny(buf, "\r\n")
			if i < 0 {
				break
			}
			line := strings.TrimSpace(string(buf[:i]))
			buf = buf[i+1:]
			if line == "" {
				continue
			}
			if _, err := master.Write([]byte(m.respond(line))); err != nil {
				return
			}
		}
	}
}

// respond returns the full reply, final result code included.
func (m *SerialModem) respond(cmd string) string {
	m.mu.Lock()
	m.commands = append(m.commands, cmd)
	m.mu.Unlock()

	ok := func(body ...string) string {
		var b strings.Builder
		for _, line := range body {
			fmt.Fprintf(&b, "\r\n%s", line)
		}
		b.WriteString("\r\n\r\nOK\r\n")
		return b.String()
	}

	switch {
	case cmd == "AT", cmd == "ATE0",
		strings.HasPrefix(cmd, "AT+CREG="), strings.HasPrefix(cmd, "AT+CGREG="),
		strings.HasPrefix(cmd, "AT+CEREG="), strings.HasPrefix(cmd, "AT+CNMI="),
		strings.HasPrefix(cmd, "AT+CMGF="), strings.HasPrefix(cmd, "AT+CMEE="):
		return ok()
	case strings.HasPrefix(cmd, `AT+CPMS="`):
		return ok(m.cpmsCounts())
	case cmd == "AT+CPMS?":
		return ok(m.cpmsReport())
	case scanSetRe.MatchString(cmd):
		return m.setScanMode(cmd)
	case scanReadRe.MatchString(cmd):
		m.mu.Lock()
		mode := m.scanMode
		m.mu.Unlock()
		return ok(fmt.Sprintf(`+QCFG: "nwscanmode",%d`, mode))
	case cmd == "AT+COPS?":
		return ok(m.copsReport())
	case copsSetRe.MatchString(cmd):
		return m.setCOPSMode(cmd)
	case cmd == "AT+CPIN?":
		return ok("+CPIN: READY")
	case cmd == "AT+CSQ":
		return ok("+CSQ: 25,99")
	case cmd == "AT+CREG?":
		return ok(m.regReport("+CREG", 7, m.statFor("+CREG")))
	case cmd == "AT+CGREG?":
		return ok(m.regReport("+CGREG", 2, m.statFor("+CGREG")))
	case cmd == "AT+CEREG?":
		return ok(m.regReport("+CEREG", 7, m.statFor("+CEREG")))
	case cmd == "AT+CIREG?":
		return m.ciregReport(ok)
	case cmd == "AT+QCCID", cmd == "AT+CCID":
		return ok("+QCCID: 8999801234567890123")
	case cmd == "AT+CIMI":
		return ok("434041234567890")
	case cmd == "AT+CNUM":
		return "\r\nERROR\r\n" // the real EC25 refuses this on these SIMs
	case strings.HasPrefix(cmd, "AT+CMGL="):
		return m.cmgl(cmd)
	case cmgdRe.MatchString(cmd):
		return m.cmgd(cmd)
	default:
		return "\r\nERROR\r\n"
	}
}

func (m *SerialModem) statFor(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch prefix {
	case "+CREG":
		return m.creg
	case "+CGREG":
		return m.cgreg
	default:
		return m.cereg
	}
}

// regReport mirrors the real modem: a registered domain names the cell it is on
// and the access technology it is on it with, an unregistered one reports the
// mode and stat alone.
func (m *SerialModem) regReport(prefix string, act, stat int) string {
	if stat == RegHome || stat == RegRoaming {
		return fmt.Sprintf(`%s: 2,%d,"10B5","BDBB208",%d`, prefix, stat, act)
	}
	return fmt.Sprintf("%s: 2,%d", prefix, stat)
}

func (m *SerialModem) ciregReport(ok func(...string) string) string {
	m.mu.Lock()
	supported := m.imsSupported
	ims := m.ims
	m.mu.Unlock()
	if !supported {
		return "\r\nERROR\r\n"
	}
	return ok(fmt.Sprintf("+CIREG: 1,%d", ims))
}

// setScanMode answers AT+QCFG="nwscanmode",<mode>[,<effect>]. A real EC25
// rejects a mode outside 0-3, and so does this.
func (m *SerialModem) setScanMode(cmd string) string {
	fields := scanSetRe.FindStringSubmatch(cmd)
	mode, err := strconv.Atoi(fields[1])
	if err != nil || mode < 0 || mode > 3 {
		return "\r\nERROR\r\n"
	}
	m.mu.Lock()
	m.scanMode = mode
	m.mu.Unlock()
	return "\r\n\r\nOK\r\n"
}

// setCOPSMode answers AT+COPS=<mode>[,...]. Only automatic selection is
// modelled; a manual lock needs an operator this fake does not have.
func (m *SerialModem) setCOPSMode(cmd string) string {
	mode, err := strconv.Atoi(copsSetRe.FindStringSubmatch(cmd)[1])
	if err != nil || mode != 0 {
		return "\r\nERROR\r\n"
	}
	m.mu.Lock()
	m.copsMode = 0
	m.mu.Unlock()
	return "\r\n\r\nOK\r\n"
}

// copsReport mirrors the real modem: automatic selection names the operator it
// settled on, a manual lock that has not registered reports the mode alone,
// which is exactly what the live backup modem returned.
func (m *SerialModem) copsReport() string {
	m.mu.Lock()
	mode := m.copsMode
	m.mu.Unlock()
	if mode == 0 {
		return `+COPS: 0,0,"beeline Beeline UZ",7`
	}
	return fmt.Sprintf("+COPS: %d", mode)
}

func (m *SerialModem) cpmsCounts() string {
	m.mu.Lock()
	used := len(m.messages)
	m.mu.Unlock()
	return fmt.Sprintf("+CPMS: %d,255,%d,255,%d,255", used, used, used)
}

func (m *SerialModem) cpmsReport() string {
	m.mu.Lock()
	used := len(m.messages)
	m.mu.Unlock()
	return fmt.Sprintf(`+CPMS: "ME",%d,255,"ME",%d,255,"ME",%d,255`, used, used, used)
}

// cmgl lists stored messages. Everything here is stat 1 (received, read);
// stat 0 returns an empty list, which is what a modem whose messages have all
// been read reports.
func (m *SerialModem) cmgl(cmd string) string {
	stat, err := strconv.Atoi(strings.TrimPrefix(cmd, "AT+CMGL="))
	if err != nil || stat != 1 {
		return "\r\n\r\nOK\r\n"
	}

	m.mu.Lock()
	indexes := make([]int, 0, len(m.messages))
	for idx := range m.messages {
		indexes = append(indexes, idx)
	}
	pdus := make(map[int]string, len(m.messages))
	for k, v := range m.messages {
		pdus[k] = v
	}
	m.mu.Unlock()

	sortInts(indexes)
	var b strings.Builder
	for _, idx := range indexes {
		hex := pdus[idx]
		// Length is the TPDU octet count, which excludes the SMSC header.
		smscOctets := 0
		if len(hex) >= 2 {
			if n, err := strconv.ParseInt(hex[:2], 16, 0); err == nil {
				smscOctets = int(n) + 1
			}
		}
		fmt.Fprintf(&b, "\r\n+CMGL: %d,1,,%d\r\n%s", idx, len(hex)/2-smscOctets, hex)
	}
	b.WriteString("\r\n\r\nOK\r\n")
	return b.String()
}

func (m *SerialModem) cmgd(cmd string) string {
	idx, err := strconv.Atoi(cmgdRe.FindStringSubmatch(cmd)[1])
	if err != nil {
		return "\r\nERROR\r\n"
	}
	m.mu.Lock()
	delete(m.messages, idx)
	m.deleted = append(m.deleted, idx)
	m.mu.Unlock()
	return "\r\n\r\nOK\r\n"
}

func sortInts(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
