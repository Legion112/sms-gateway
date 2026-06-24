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
		if err := messagesOne(target); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func messagesOne(target cmdutil.ModemHandle) error {
	ctx, cancel := cmdutil.SMSContext(target.Config)
	defer cancel()

	messages, err := target.Modem.ListMessages(ctx)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
	}

	fmt.Printf("driver: %s\n", target.Config.Driver)
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

func closeModems(targets []cmdutil.ModemHandle) {
	for _, t := range targets {
		_ = t.Modem.Close()
	}
}
