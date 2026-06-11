package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/driver/serial"
	"github.com/legion/sms-gateway/internal/factory"
)

const (
	exitOK    = 0
	exitModem = 1
	exitSetup = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath = flag.String("config", "", "config file path")
		driver     = flag.String("driver", "", "driver override: mm | serial")
		device     = flag.String("device", "", "serial device (serial driver only)")
		timeout    = flag.Duration("timeout", 0, "command timeout")
		modemIndex = flag.Int("modem-index", -1, "ModemManager modem index (mm driver only)")
		verbose    = flag.Bool("verbose", false, "verbose logging")
		listPorts  = flag.Bool("list-ports", false, "list serial ports and exit")
	)
	flag.Parse()

	if *listPorts {
		return listSerialPorts()
	}

	var indexOverride *int
	if *modemIndex >= 0 {
		indexOverride = modemIndex
	}

	cfg, err := config.Load(config.Overrides{
		ConfigPath: *configPath,
		Driver:     *driver,
		Device:     *device,
		Timeout:    *timeout,
		ModemIndex: indexOverride,
		Verbose:    *verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitSetup
	}

	m, err := factory.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitSetup
	}
	defer m.Close()

	pingTimeout := cfg.MM.Timeout
	if cfg.Driver == config.DriverSerial {
		pingTimeout = cfg.Serial.Timeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout*2)
	defer cancel()

	result, err := m.Ping(ctx)
	if err != nil {
		fmt.Printf("driver: %s\n", cfg.Driver)
		if result.Device != "" {
			fmt.Printf("device: %s\n", result.Device)
		}
		fmt.Printf("status: failed\n")
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitModem
	}

	fmt.Printf("driver: %s\n", result.Driver)
	fmt.Printf("device: %s\n", result.Device)
	fmt.Printf("status: ok\n")
	fmt.Printf("detail: %s\n", result.Detail)
	return exitOK
}

func listSerialPorts() int {
	ports, err := serial.ListPorts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: list ports: %v\n", err)
		return exitSetup
	}
	if len(ports) == 0 {
		fmt.Println("no serial ports found")
		return exitSetup
	}
	for _, p := range ports {
		fmt.Println(p)
	}
	return exitOK
}
