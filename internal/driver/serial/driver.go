package serial

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"

	"go.bug.st/serial"
)

const defaultBaudRate = 115200

// Driver talks to a Quectel modem over a serial AT port.
type Driver struct {
	device  string
	port    serial.Port
	timeout time.Duration
	verbose bool
	log     func(format string, args ...any)
}

// New opens the serial AT connection (with auto port probing when configured).
func New(cfg config.SerialConfig, verbose bool) (modem.Modem, error) {
	if cfg.BaudRate == 0 {
		cfg.BaudRate = defaultBaudRate
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Device == "" {
		cfg.Device = "auto"
	}

	logFn := func(string, ...any) {}
	if verbose {
		logFn = log.Printf
	}

	clientCfg := openConfig{
		Device:   cfg.Device,
		BaudRate: cfg.BaudRate,
		Timeout:  cfg.Timeout,
		Verbose:  verbose,
		Log:      logFn,
	}

	d := &Driver{timeout: cfg.Timeout, verbose: verbose, log: logFn}
	port, device, err := openAuto(clientCfg)
	if err != nil {
		return nil, err
	}
	d.port = port
	d.device = device
	return d, nil
}

// Ping sends AT and expects OK.
func (d *Driver) Ping(ctx context.Context) (modem.PingResult, error) {
	if _, err := d.exec(ctx, "ATE0"); err != nil {
		return modem.PingResult{}, fmt.Errorf("disable echo: %w", err)
	}
	if _, err := d.exec(ctx, "AT"); err != nil {
		return modem.PingResult{}, err
	}
	return modem.PingResult{
		Driver: modem.DriverSerial,
		Detail: "OK",
		Device: d.device,
	}, nil
}

// Close releases the serial port.
func (d *Driver) Close() error {
	if d.port == nil {
		return nil
	}
	err := d.port.Close()
	d.port = nil
	return err
}

// ListPorts returns detected serial port device paths.
func ListPorts() ([]string, error) {
	return serial.GetPortsList()
}

type openConfig struct {
	Device   string
	BaudRate int
	Timeout  time.Duration
	Verbose  bool
	Log      func(format string, args ...any)
}

func openPort(cfg openConfig) (serial.Port, error) {
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
	if err := port.SetReadTimeout(100 * time.Millisecond); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("set read timeout: %w", err)
	}
	return port, nil
}

func (d *Driver) exec(ctx context.Context, command string) (string, error) {
	line := command + "\r\n"
	if d.verbose {
		d.log("TX: %q", strings.TrimSpace(line))
	}

	if _, err := d.port.Write([]byte(line)); err != nil {
		return "", fmt.Errorf("write command: %w", err)
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(d.timeout)
	}

	var buf strings.Builder
	reader := bufio.NewReader(d.port)

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

		if d.verbose {
			d.log("RX: %q", trimmed)
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
