package cli

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/driver/serial"
	"github.com/spf13/cobra"
)

func newPortsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports",
		Short: "List detected serial ports",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPorts()
		},
	}
}

func runPorts() error {
	ports, err := serial.ListPorts()
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: list ports: %v", err))
	}
	if len(ports) == 0 {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, "no serial ports found")
	}
	for _, p := range ports {
		fmt.Println(p)
	}
	return nil
}
