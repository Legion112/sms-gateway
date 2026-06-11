package cli

import (
	"fmt"
	"os"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/spf13/cobra"
)

var rootFlags cmdutil.Flags

var rootCmd = &cobra.Command{
	Use:           "sms-gateway",
	Short:         "SMS gateway for Quectel EC25",
	Long:          "SMS gateway for receiving SMS on Linux using a Quectel EC25-EUX LTE modem.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and returns the process exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return cmdutil.ExitCode(err)
	}
	return cmdutil.ExitOK
}

func init() {
	cmdutil.BindPersistentFlags(rootCmd, &rootFlags)
	rootCmd.AddCommand(newPingCmd(&rootFlags))
	rootCmd.AddCommand(newStatusCmd(&rootFlags))
	rootCmd.AddCommand(newMessagesCmd(&rootFlags))
	rootCmd.AddCommand(newPortsCmd())
}
