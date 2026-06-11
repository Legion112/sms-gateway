package cmdutil

import (
	"errors"

	"github.com/spf13/cobra"
)

// Exit codes shared by CLI commands.
const (
	ExitOK    = 0
	ExitModem = 1
	ExitSetup = 2
)

// CLIError carries an exit code for Cobra command handlers.
type CLIError struct {
	Code int
	Msg  string
}

func (e *CLIError) Error() string {
	return e.Msg
}

// NewCLIError returns an error that maps to a specific exit code.
func NewCLIError(code int, msg string) error {
	return &CLIError{Code: code, Msg: msg}
}

// ExitCode extracts the exit code from an error, defaulting to ExitSetup.
func ExitCode(err error) int {
	var cliErr *CLIError
	if errors.As(err, &cliErr) {
		return cliErr.Code
	}
	return ExitSetup
}

// BindPersistentFlags attaches global flags to a Cobra command.
func BindPersistentFlags(cmd *cobra.Command, f *Flags) {
	cmd.PersistentFlags().StringVar(&f.ConfigPath, "config", "", "config file path")
	cmd.PersistentFlags().StringVar(&f.Driver, "driver", "", "driver override: mm | serial")
	cmd.PersistentFlags().BoolVarP(&f.Verbose, "verbose", "v", false, "verbose logging")
}

// BindModemFlags attaches per-command modem flags.
func BindModemFlags(cmd *cobra.Command, f *Flags) {
	cmd.Flags().StringVar(&f.Device, "device", "", "serial device (serial driver only)")
	cmd.Flags().DurationVar(&f.Timeout, "timeout", 0, "command timeout")
	cmd.Flags().IntVar(&f.ModemIndex, "modem-index", -1, "ModemManager modem index (mm driver only)")
}
