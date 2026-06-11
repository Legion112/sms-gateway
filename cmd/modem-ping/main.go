package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/driver/serial"
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
	var flags cmdutil.Flags
	cmdutil.RegisterFlags(flag.CommandLine, &flags)
	listPorts := flag.Bool("list-ports", false, "list serial ports and exit")
	flag.Parse()

	if *listPorts {
		return listSerialPorts()
	}

	cfg, m, err := cmdutil.OpenModem(flags)
	if err != nil {
		cmdutil.PrintError("error: %v", err)
		return exitSetup
	}
	defer m.Close()

	ctx, cancel := cmdutil.Context(cfg)
	defer cancel()

	result, err := m.Ping(ctx)
	if err != nil {
		fmt.Printf("driver: %s\n", cfg.Driver)
		if result.Device != "" {
			fmt.Printf("device: %s\n", result.Device)
		}
		fmt.Printf("status: failed\n")
		cmdutil.PrintError("error: %v", err)
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
		cmdutil.PrintError("error: list ports: %v", err)
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
