package serial

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
)

var cmgsRe = regexp.MustCompile(`\+CMGS:\s*(\d+)`)

// SendMessage sends an SMS via AT+CMGS.
func (d *Driver) SendMessage(ctx context.Context, number, text string) (modem.SendResult, error) {
	if number == "" {
		return modem.SendResult{}, fmt.Errorf("number is required")
	}
	if text == "" {
		return modem.SendResult{}, fmt.Errorf("text is required")
	}

	if _, err := d.exec(ctx, "ATE0"); err != nil {
		return modem.SendResult{}, fmt.Errorf("disable echo: %w", err)
	}
	if _, err := d.exec(ctx, "AT+CMGF=1"); err != nil {
		return modem.SendResult{}, fmt.Errorf("text mode: %w", err)
	}

	resp, err := d.execCMGS(ctx, number, text)
	if err != nil {
		return modem.SendResult{}, err
	}

	id := ""
	if m := cmgsRe.FindStringSubmatch(resp); len(m) == 2 {
		id = m[1]
	}

	return modem.SendResult{
		ID:    id,
		To:    number,
		State: "sent",
		Text:  text,
	}, nil
}

func (d *Driver) execCMGS(ctx context.Context, number, text string) (string, error) {
	cmd := fmt.Sprintf(`AT+CMGS=%q`, number)
	if d.verbose {
		d.log("TX: %q", cmd)
	}
	if _, err := d.port.Write([]byte(cmd + "\r\n")); err != nil {
		return "", fmt.Errorf("write CMGS: %w", err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(d.timeout * 3)
	}

	reader := bufio.NewReader(d.port)
	if err := waitPrompt(reader, deadline, d.verbose, d.log); err != nil {
		return "", err
	}

	payload := text + string('\x1a')
	if d.verbose {
		d.log("TX: %q", text+"<SUB>")
	}
	if _, err := d.port.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("write text: %w", err)
	}

	return readUntilDone(reader, deadline, d.verbose, d.log)
}

func waitPrompt(reader *bufio.Reader, deadline time.Time, verbose bool, log func(string, ...any)) error {
	for time.Now().Before(deadline) {
		b, err := reader.ReadByte()
		if err != nil {
			if isTimeout(err) {
				continue
			}
			if errors.Is(err, io.EOF) {
				continue
			}
			return fmt.Errorf("read prompt: %w", err)
		}
		if verbose {
			log("RX: %q", string(b))
		}
		if b == '>' {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for CMGS prompt")
}

func readUntilDone(reader *bufio.Reader, deadline time.Time, verbose bool, log func(string, ...any)) (string, error) {
	var buf strings.Builder
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) || isTimeout(err) {
				continue
			}
			return buf.String(), fmt.Errorf("read response: %w", err)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if verbose {
			log("RX: %q", trimmed)
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(trimmed)

		if cmeErr, ok := parseCMEError(trimmed); ok {
			return buf.String(), fmt.Errorf("modem returned %s", cmeErr)
		}
		switch parseResultCode(trimmed) {
		case resultOK:
			return buf.String(), nil
		case resultError:
			return buf.String(), fmt.Errorf("modem returned ERROR")
		}
	}
	if buf.Len() == 0 {
		return "", fmt.Errorf("timeout waiting for send confirmation")
	}
	return buf.String(), fmt.Errorf("timeout waiting for OK (partial: %q)", buf.String())
}
