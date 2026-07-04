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
)

var (
	cmglHeaderRe  = regexp.MustCompile(`^\+CMGL:\s*(\d+),"([^"]*)","([^"]*)",[^,\n]*,(.+)$`)
	qcmgrHeaderRe = regexp.MustCompile(`^\+QCMGR:\s*"([^"]*)","([^"]*)",[^,\n]*,(.+)$`)
)

// ListMessages reads all stored SMS via AT+CMGL and AT+QCMGR (concat metadata).
func (d *Driver) ListMessages(ctx context.Context) ([]modem.Message, error) {
	if err := d.setup(ctx); err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}
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
	case "REC UNREAD":
		return "received (unread)"
	case "REC READ":
		return "received (read)"
	case "STO UNSENT":
		return "stored (unsent)"
	case "STO SENT":
		return "stored (sent)"
	default:
		return strings.ToLower(raw)
	}
}
