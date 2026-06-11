package modem

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.bug.st/serial"
)

const (
	DefaultDevice   = "/dev/ttyUSB2"
	DefaultBaudRate = 115200
)

// Client talks to a Quectel modem over a serial AT port.
type Client struct {
	port    serial.Port
	timeout time.Duration
	verbose bool
	log     func(format string, args ...any)
}

// Config holds serial connection settings.
type Config struct {
	Device   string
	BaudRate int
	Timeout  time.Duration
	Verbose  bool
	Log      func(format string, args ...any)
}

// Open connects to the modem on the given serial device.
func Open(cfg Config) (*Client, error) {
	if cfg.Device == "" {
		cfg.Device = DefaultDevice
	}
	if cfg.BaudRate == 0 {
		cfg.BaudRate = DefaultBaudRate
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = func(string, ...any) {}
	}

	mode := &serial.Mode{
		BaudRate: cfg.BaudRate,
		Parity:   serial.NoParity,
		DataBits: 8,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(cfg.Device, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Device, err)
	}

	c := &Client{
		port:    port,
		timeout: cfg.Timeout,
		verbose: cfg.Verbose,
		log:     cfg.Log,
	}

	if err := c.port.SetReadTimeout(100 * time.Millisecond); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("set read timeout: %w", err)
	}

	return c, nil
}

// Close releases the serial port.
func (c *Client) Close() error {
	if c.port == nil {
		return nil
	}
	err := c.port.Close()
	c.port = nil
	return err
}

// Ping sends AT and expects OK. Disables echo first for cleaner responses.
func (c *Client) Ping(ctx context.Context) (string, error) {
	if _, err := c.exec(ctx, "ATE0"); err != nil {
		return "", fmt.Errorf("disable echo: %w", err)
	}
	return c.exec(ctx, "AT")
}

func (c *Client) exec(ctx context.Context, command string) (string, error) {
	line := command + "\r\n"
	if c.verbose {
		c.log("TX: %q", strings.TrimSpace(line))
	}

	if _, err := c.port.Write([]byte(line)); err != nil {
		return "", fmt.Errorf("write command: %w", err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.timeout)
	}

	var buf strings.Builder
	reader := bufio.NewReader(c.port)

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return buf.String(), err
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				continue
			}
			if !isTimeout(err) {
				return buf.String(), fmt.Errorf("read response: %w", err)
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if c.verbose {
			c.log("RX: %q", trimmed)
		}

		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(trimmed)

		switch parseResultCode(trimmed) {
		case resultOK:
			return buf.String(), nil
		case resultError:
			return buf.String(), fmt.Errorf("modem returned ERROR")
		case resultNone:
			continue
		}
	}

	if buf.Len() == 0 {
		return "", fmt.Errorf("timeout waiting for modem response")
	}
	return buf.String(), fmt.Errorf("timeout waiting for OK/ERROR (partial: %q)", buf.String())
}

type resultCode int

const (
	resultNone resultCode = iota
	resultOK
	resultError
)

// parseResultCode checks whether a response line is a final AT result code.
func parseResultCode(line string) resultCode {
	switch strings.ToUpper(strings.TrimSpace(line)) {
	case "OK":
		return resultOK
	case "ERROR":
		return resultError
	default:
		return resultNone
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out")
}
