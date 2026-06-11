package cli

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/spf13/cobra"
)

func newMessagesCmd(flags *cmdutil.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "List all SMS messages",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMessages(*flags)
		},
	}
	cmdutil.BindModemFlags(cmd, flags)
	return cmd
}

func runMessages(flags cmdutil.Flags) error {
	cfg, m, err := cmdutil.OpenModem(flags)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}
	defer m.Close()

	ctx, cancel := cmdutil.SMSContext(cfg)
	defer cancel()

	messages, err := m.ListMessages(ctx)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
	}

	fmt.Printf("driver: %s\n", cfg.Driver)
	fmt.Printf("count: %d\n", len(messages))
	if len(messages) == 0 {
		return nil
	}

	for i, msg := range messages {
		if i > 0 {
			fmt.Println("---")
		}
		printMessage(msg)
	}
	return nil
}

func printMessage(msg modem.Message) {
	fmt.Printf("id: %s\n", msg.ID)
	if msg.From != "" {
		fmt.Printf("from: %s\n", msg.From)
	}
	if msg.State != "" {
		fmt.Printf("state: %s\n", msg.State)
	}
	if msg.Timestamp != "" {
		fmt.Printf("timestamp: %s\n", msg.Timestamp)
	}
	if msg.Storage != "" {
		fmt.Printf("storage: %s\n", msg.Storage)
	}
	if msg.SMSC != "" {
		fmt.Printf("smsc: %s\n", msg.SMSC)
	}
	if msg.Text != "" {
		fmt.Printf("text: %s\n", msg.Text)
	}
}
