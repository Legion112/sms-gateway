package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/spf13/cobra"
)

type channelTestFlags struct {
	text string
}

func newChannelCmd(root *cmdutil.Flags) *cobra.Command {
	testFlags := channelTestFlags{}

	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Forward channel commands",
	}

	testCmd := &cobra.Command{
		Use:   "test [channel-name]",
		Short: "Send a test message through a forward channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChannelTest(root, testFlags, args[0])
		},
	}
	testCmd.Flags().StringVar(&testFlags.text, "text", "sms-gateway channel test OK", "test message body")

	cmd.AddCommand(testCmd)
	return cmd
}

func runChannelTest(root *cmdutil.Flags, f channelTestFlags, channelName string) error {
	cfg, err := cmdutil.LoadConfig(*root)
	if err != nil {
		return err
	}

	chCfg, ok := cfg.Channels[channelName]
	if !ok {
		return fmt.Errorf("channel %q not found in config", channelName)
	}

	ch, err := forward.NewChannel(channelName, chCfg, cfg.Verbose)
	if err != nil {
		return err
	}
	defer ch.Close()

	ctx, cancel := contextWithTimeout(45 * time.Second)
	defer cancel()

	if err := ch.Ping(ctx); err != nil {
		cmdutil.PrintError("status: failed")
		cmdutil.PrintError("error: %v", err)
		return err
	}

	if err := ch.SendTest(ctx, f.text); err != nil {
		cmdutil.PrintError("status: failed")
		cmdutil.PrintError("channel: %s", channelName)
		cmdutil.PrintError("error: %v", err)
		return err
	}

	fmt.Printf("status: ok\n")
	fmt.Printf("channel: %s\n", channelName)
	fmt.Printf("driver: %s\n", ch.Driver())
	if chCfg.Driver == forward.DriverTelegramBot {
		fmt.Printf("chat_id: %d\n", chCfg.Telegram.ChatID)
	}
	return nil
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
