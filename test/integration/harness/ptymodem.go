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
}

var cmgdRe = regexp.MustCompile(`^AT\+CMGD=(\d+)`)

// NewSerialModem creates the pty and the device node, and starts answering AT.
func NewSerialModem(link string) (*SerialModem, error) {
	m := &SerialModem{link: link, messages: map[int]string{}}
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
	case cmd == "AT+CPIN?":
		return ok("+CPIN: READY")
	case cmd == "AT+CSQ":
		return ok("+CSQ: 25,99")
	case cmd == "AT+CREG?":
		return ok(`+CREG: 2,1,"10B5","BDBB208",7`)
	case cmd == "AT+CGREG?":
		return ok(`+CGREG: 2,1,"10B5","BDBB208",7`)
	case cmd == "AT+CEREG?":
		return ok(`+CEREG: 2,1,"10B5","BDBB208",7`)
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
