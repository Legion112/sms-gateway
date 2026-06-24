package cli

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newPingCmd(flags *cmdutil.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ping",
		Short: "Check modem connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPing(*flags)
		},
	}
	cmdutil.BindModemFlags(cmd, flags)
	return cmd
}

func runPing(flags cmdutil.Flags) error {
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
		if err := pingOne(target); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func pingOne(target cmdutil.ModemHandle) error {
	ctx, cancel := cmdutil.Context(target.Config)
	defer cancel()

	result, err := target.Modem.Ping(ctx)
	if err != nil {
		fmt.Printf("driver: %s\n", target.Config.Driver)
		if result.Device != "" {
			fmt.Printf("device: %s\n", result.Device)
		}
		fmt.Printf("status: failed\n")
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
	}

	fmt.Printf("driver: %s\n", result.Driver)
	fmt.Printf("device: %s\n", result.Device)
	fmt.Printf("status: ok\n")
	fmt.Printf("detail: %s\n", result.Detail)
	return nil
}
