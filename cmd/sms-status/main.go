package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/legion/sms-gateway/internal/cmdutil"
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
	flag.Parse()

	cfg, m, err := cmdutil.OpenModem(flags)
	if err != nil {
		cmdutil.PrintError("error: %v", err)
		return exitSetup
	}
	defer m.Close()

	ctx, cancel := cmdutil.SMSContext(cfg)
	defer cancel()

	status, err := m.SMSStatus(ctx)
	if err != nil {
		fmt.Printf("driver: %s\n", cfg.Driver)
		if status.Device != "" {
			fmt.Printf("device: %s\n", status.Device)
		}
		fmt.Printf("status: failed\n")
		cmdutil.PrintError("error: %v", err)
		return exitModem
	}

	fmt.Printf("driver: %s\n", status.Driver)
	fmt.Printf("device: %s\n", status.Device)
	fmt.Printf("sim: %s\n", status.SimStatus)
	fmt.Printf("modem: %s\n", status.ModemState)
	fmt.Printf("network: %s\n", status.NetworkState)
	fmt.Printf("messages: %s\n", status.MessageStore)
	if status.MessageCount >= 0 {
		fmt.Printf("message_count: %d\n", status.MessageCount)
	}
	fmt.Printf("sms_ready: %t\n", status.SMSReady)
	fmt.Printf("detail: %s\n", status.Detail)
	return exitOK
}
