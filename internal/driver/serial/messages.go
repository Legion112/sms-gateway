package serial

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/legion/sms-gateway/internal/modem"
)

var cmglHeaderRe = regexp.MustCompile(`^\+CMGL:\s*(\d+),"([^"]*)","([^"]*)","[^"]*","([^"]*)"`)

// ListMessages reads all stored SMS via AT+CMGL.
func (d *Driver) ListMessages(ctx context.Context) ([]modem.Message, error) {
	if _, err := d.exec(ctx, "ATE0"); err != nil {
		return nil, fmt.Errorf("disable echo: %w", err)
	}
	if _, err := d.exec(ctx, "AT+CMGF=1"); err != nil {
		return nil, fmt.Errorf("text mode: %w", err)
	}

	resp, err := d.exec(ctx, `AT+CMGL="ALL"`)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}

	return parseCMGL(resp), nil
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
			Timestamp: m[4],
			Text:      text,
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
