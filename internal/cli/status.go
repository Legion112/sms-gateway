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
	cfg, m, err := cmdutil.OpenModem(flags)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
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
