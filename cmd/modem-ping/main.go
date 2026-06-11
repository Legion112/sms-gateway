package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/legion/sms-gateway/internal/modem"

	"go.bug.st/serial"
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
	device := flag.String("device", envOr("MODEM_DEVICE", modem.DefaultDevice), "serial device path")
	timeout := flag.Duration("timeout", 2*time.Second, "per-command timeout")
	verbose := flag.Bool("verbose", false, "log raw AT traffic to stderr")
	listPorts := flag.Bool("list-ports", false, "list serial ports and exit")
	flag.Parse()

	if *listPorts {
		return listSerialPorts()
	}

	logFn := func(format string, args ...any) {
		if *verbose {
			log.Printf(format, args...)
		}
	}

	client, err := modem.Open(modem.Config{
		Device:  *device,
		Timeout: *timeout,
		Verbose: *verbose,
		Log:     logFn,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitSetup
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout*2)
	defer cancel()

	response, err := client.Ping(ctx)
	if err != nil {
		fmt.Printf("device: %s\n", *device)
		fmt.Printf("status: failed\n")
		if response != "" {
			fmt.Printf("response: %s\n", response)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitModem
	}

	fmt.Printf("device: %s\n", *device)
	fmt.Printf("status: ok\n")
	fmt.Printf("response: OK\n")
	return exitOK
}

func listSerialPorts() int {
	ports, err := serial.GetPortsList()
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
