package serial

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/smsconcat"
	"github.com/legion/sms-gateway/internal/smsencoding"
	"github.com/legion/sms-gateway/internal/smspdu"
)

var (
	cmglHeaderRe    = regexp.MustCompile(`^\+CMGL:\s*(\d+),"([^"]*)","([^"]*)",[^,\n]*,(.+)$`)
	cmglPDUHeaderRe = regexp.MustCompile(`^\+CMGL:\s*(\d+),(\d+),,(\d+)\s*$`)
	qcmgrHeaderRe   = regexp.MustCompile(`^\+QCMGR:\s*"([^"]*)","([^"]*)",[^,\n]*,(.+)$`)
)

// ListMessages reads stored SMS via PDU-mode CMGL, falling back to text-mode QCMGR.
func (d *Driver) ListMessages(ctx context.Context) ([]modem.Message, error) {
	if err := d.setup(ctx); err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}

	parts, err := d.listPDUParts(ctx)
	if err == nil {
		return smsconcat.Merge(parts), nil
	}
	if err != nil && d.verbose {
		d.log("pdu list failed: %v, falling back to text mode", err)
	}

	return d.listMessagesText(ctx)
}

func (d *Driver) listPDUParts(ctx context.Context) ([]smsconcat.Part, error) {
	if _, err := d.exec(ctx, "AT+CMGF=0"); err != nil {
		return nil, fmt.Errorf("pdu mode: %w", err)
	}
	defer func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), d.timeout*2)
		defer cancel()
		if _, err := d.exec(restoreCtx, "AT+CMGF=1"); err != nil && d.verbose {
			d.log("restore text mode: %v", err)
		}
	}()

	var parts []smsconcat.Part
	for stat, state := range map[int]string{0: "received (unread)", 1: "received (read)"} {
		statParts, err := d.listPDUPartsByStat(ctx, stat, state)
		if err != nil {
			return nil, err
		}
		parts = append(parts, statParts...)
	}
	return parts, nil
}

func (d *Driver) listPDUPartsByStat(ctx context.Context, stat int, state string) ([]smsconcat.Part, error) {
	resp, err := d.exec(ctx, fmt.Sprintf("AT+CMGL=%d", stat))
	if err != nil {
		return nil, fmt.Errorf("cmgl stat %d: %w", stat, err)
	}
	entries := parseCMGLPDU(resp)
	parts := make([]smsconcat.Part, 0, len(entries))
	for _, entry := range entries {
		deliver, err := smspdu.ParseHexDeliver(entry.hexPDU)
		if err != nil {
			return nil, fmt.Errorf("parse pdu index %s: %w", entry.index, err)
		}
		parts = append(parts, smsconcat.Part{
			ID:        entry.index,
			From:      deliver.From,
			State:     state,
			Timestamp: deliver.Timestamp,
			Text:      deliver.Text,
			Storage:   "modem",
			ConcatRef: deliver.ConcatRef,
			Part:      deliver.Part,
			PartTotal: deliver.PartTotal,
		})
	}
	return parts, nil
}

type cmglPDUEntry struct {
	index  string
	stat   string
	length int
	hexPDU string
}

func parseCMGLPDU(response string) []cmglPDUEntry {
	lines := strings.Split(response, "\n")
	var entries []cmglPDUEntry
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		m := cmglPDUHeaderRe.FindStringSubmatch(line)
		if len(m) != 4 {
			continue
		}
		length, _ := strconv.Atoi(m[3])
		if i+1 >= len(lines) {
			continue
		}
		hexLine := strings.Map(func(r rune) rune {
			switch {
			case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
				return r
			default:
				return -1
			}
		}, strings.TrimSpace(lines[i+1]))
		if hexLine == "" {
			continue
		}
		if length > 0 && len(hexLine) != length*2 {
			// Some modems report TPDU length; accept the hex line we got.
		}
		entries = append(entries, cmglPDUEntry{
			index:  m[1],
			stat:   m[2],
			length: length,
			hexPDU: hexLine,
		})
		i++
	}
	return entries
}

func (d *Driver) listMessagesText(ctx context.Context) ([]modem.Message, error) {
	if _, err := d.exec(ctx, "AT+CMGF=1"); err != nil {
		return nil, fmt.Errorf("text mode: %w", err)
	}

	resp, err := d.exec(ctx, `AT+CMGL="ALL"`)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	indices := parseCMGLIndices(resp)
	if len(indices) == 0 {
		return nil, nil
	}

	parts, qcmgrOK := d.readQCMGRParts(ctx, indices)
	if !qcmgrOK {
		return parseCMGL(resp), nil
	}
	return smsconcat.Merge(parts), nil
}

func parseCMGLIndices(response string) []string {
	var indices []string
	for _, line := range strings.Split(response, "\n") {
		m := cmglHeaderRe.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) == 5 {
			indices = append(indices, m[1])
		}
	}
	return indices
}

func (d *Driver) readQCMGRParts(ctx context.Context, indices []string) ([]smsconcat.Part, bool) {
	parts := make([]smsconcat.Part, 0, len(indices))
	for _, index := range indices {
		resp, err := d.exec(ctx, "AT+QCMGR="+index)
		if err != nil {
			return nil, false
		}
		part, ok := parseQCMGR(index, resp)
		if !ok {
			return nil, false
		}
		parts = append(parts, part)
	}
	return parts, true
}

func parseQCMGR(index, response string) (smsconcat.Part, bool) {
	lines := strings.Split(response, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		m := qcmgrHeaderRe.FindStringSubmatch(line)
		if len(m) != 4 {
			continue
		}
		text := ""
		if i+1 < len(lines) {
			text = strings.TrimSpace(lines[i+1])
		}

		timestamp, concatRef, partNum, partTotal := parseQCMGRTail(m[3])
		return smsconcat.Part{
			ID:        index,
			From:      m[2],
			State:     cmglStatus(m[1]),
			Timestamp: timestamp,
			Text:      smsencoding.DecodeText(text),
			Storage:   "modem",
			ConcatRef: concatRef,
			Part:      partNum,
			PartTotal: partTotal,
		}, true
	}
	return smsconcat.Part{}, false
}

func parseQCMGRTail(tail string) (timestamp string, concatRef, partNum, partTotal int) {
	tail = strings.TrimSpace(tail)
	tail = strings.Trim(tail, `"`)

	fields := splitTrailingFields(tail)
	if len(fields) == 0 {
		return tail, 0, 0, 0
	}

	if len(fields) >= 4 {
		ref, err1 := strconv.Atoi(fields[len(fields)-3])
		seg, err2 := strconv.Atoi(fields[len(fields)-2])
		total, err3 := strconv.Atoi(fields[len(fields)-1])
		if err1 == nil && err2 == nil && err3 == nil && total >= 2 && seg >= 1 {
			ts := strings.Join(fields[:len(fields)-3], ",")
			return strings.Trim(ts, `"`), ref, seg, total
		}
	}

	return strings.Trim(tail, `"`), 0, 0, 0
}

func splitTrailingFields(tail string) []string {
	return strings.Split(tail, ",")
}

func parseCMGL(response string) []modem.Message {
	lines := strings.Split(response, "\n")
	var messages []modem.Message

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		m := cmglHeaderRe.FindStringSubmatch(line)
		if len(m) != 5 {
			continue
		}
		text := ""
		if i+1 < len(lines) {
			text = strings.TrimSpace(lines[i+1])
			i++
		}
		messages = append(messages, modem.Message{
			ID:        m[1],
			State:     cmglStatus(m[2]),
			From:      m[3],
			Timestamp: strings.Trim(m[4], `"`),
			Text:      smsencoding.DecodeText(text),
			Storage:   "modem",
		})
	}
	return messages
}

func cmglStatus(raw string) string {
	switch strings.ToUpper(raw) {
	case "REC UNREAD", "0":
		return "received (unread)"
	case "REC READ", "1":
		return "received (read)"
	case "STO UNSENT":
		return "stored (unsent)"
	case "STO SENT":
		return "stored (sent)"
	default:
		return strings.ToLower(raw)
	}
}
