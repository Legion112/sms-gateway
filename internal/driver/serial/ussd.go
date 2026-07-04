package serial

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// QueryPhoneUSSD sends a carrier USSD code and extracts a phone number from the reply.
func (d *Driver) QueryPhoneUSSD(ctx context.Context, ussdCode string) (string, error) {
	if ussdCode == "" {
		return "", fmt.Errorf("ussd code is required")
	}
	if err := d.setup(ctx); err != nil {
		return "", err
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	minDeadline := time.Now().Add(25 * time.Second)
	if deadline.Before(minDeadline) {
		deadline = minDeadline
	}

	cmd := fmt.Sprintf(`AT+CUSD=1,%q,15`, ussdCode)
	if d.verbose {
		d.log("TX: %q", cmd)
	}
	if _, err := d.port.Write([]byte(cmd + "\r\n")); err != nil {
		return "", fmt.Errorf("write USSD: %w", err)
	}

	var buf strings.Builder
	reader := bufio.NewReader(d.port)
	gotCUSD := false

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) || isTimeout(err) {
				continue
			}
			return "", fmt.Errorf("read USSD response: %w", err)
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if d.verbose {
			d.log("RX: %q", trimmed)
		}
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(trimmed)

		if strings.HasPrefix(trimmed, "+CUSD:") {
			gotCUSD = true
		}
		if cmeErr, ok := parseCMEError(trimmed); ok {
			return "", fmt.Errorf("modem returned %s", cmeErr)
		}
		if gotCUSD && parseResultCode(trimmed) == resultOK {
			break
		}
		if parseResultCode(trimmed) == resultError {
			return "", fmt.Errorf("modem returned ERROR")
		}
	}

	text := parseCUSD(buf.String())
	if text == "" {
		return "", fmt.Errorf("no USSD response for %q", ussdCode)
	}
	phone := extractPhoneFromText(text)
	if phone == "" {
		return "", fmt.Errorf("USSD response contained no phone number: %q", text)
	}
	return phone, nil
}
