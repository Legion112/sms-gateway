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
	cfg, m, err := cmdutil.OpenModem(flags)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
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
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
	}

	fmt.Printf("driver: %s\n", result.Driver)
	fmt.Printf("device: %s\n", result.Device)
	fmt.Printf("status: ok\n")
	fmt.Printf("detail: %s\n", result.Detail)
	return nil
}
