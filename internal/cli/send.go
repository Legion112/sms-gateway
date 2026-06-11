package cli

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/spf13/cobra"
)

func newSendCmd(flags *cmdutil.Flags) *cobra.Command {
	var number, text string

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send an SMS message",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSend(*flags, number, text)
		},
	}

	cmd.Flags().StringVar(&number, "number", "", "destination phone number (required)")
	cmd.Flags().StringVar(&text, "text", "", "message text (required)")
	_ = cmd.MarkFlagRequired("number")
	_ = cmd.MarkFlagRequired("text")
	cmdutil.BindModemFlags(cmd, flags)
	return cmd
}

func runSend(flags cmdutil.Flags, number, text string) error {
	cfg, m, err := cmdutil.OpenModem(flags)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}
	defer m.Close()

	ctx, cancel := cmdutil.SendContext(cfg)
	defer cancel()

	result, err := m.SendMessage(ctx, number, text)
	if err != nil {
		fmt.Printf("driver: %s\n", cfg.Driver)
		fmt.Printf("to: %s\n", number)
		fmt.Printf("status: failed\n")
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
	}

	fmt.Printf("driver: %s\n", cfg.Driver)
	fmt.Printf("status: ok\n")
	fmt.Printf("to: %s\n", result.To)
	if result.ID != "" {
		fmt.Printf("id: %s\n", result.ID)
	}
	fmt.Printf("state: %s\n", result.State)
	fmt.Printf("text: %s\n", result.Text)
	return nil
}
