package cli

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newStatusCmd(flags *cmdutil.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show SIM and SMS readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(*flags)
		},
	}
	cmdutil.BindModemFlags(cmd, flags)
	return cmd
}

func runStatus(flags cmdutil.Flags) error {
	_, targets, err := cmdutil.OpenModemTargets(flags)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}
	defer closeModems(targets)

	multi := len(targets) > 1
	var firstErr error
	for i, target := range targets {
		if i > 0 {
			fmt.Println()
		}
		cmdutil.PrintSection(target.Name, multi)
		if err := statusOne(target); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func statusOne(target cmdutil.ModemHandle) error {
	ctx, cancel := cmdutil.SMSContext(target.Config)
	defer cancel()

	status, err := target.Modem.SMSStatus(ctx)
	if err != nil {
		fmt.Printf("driver: %s\n", target.Config.Driver)
		if status.Device != "" {
			fmt.Printf("device: %s\n", status.Device)
		}
		fmt.Printf("status: failed\n")
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
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
	return nil
}
