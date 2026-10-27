package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/smsconcat"
	"github.com/legion/sms-gateway/internal/storage"
	"github.com/spf13/cobra"
)

type pruneOptions struct {
	apply bool
	all   bool
}

func newPruneCmd(flags *cmdutil.Flags) *cobra.Command {
	var opts pruneOptions
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Free modem storage slots holding already-forwarded SMS",
		Long: "Delete SMS from the modem's own storage once the local database shows " +
			"every channel was delivered. Reports what it would delete unless --yes is given.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(*flags, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.apply, "yes", false, "actually delete (default is a dry run)")
	cmd.Flags().BoolVar(&opts.all, "all", false, "also delete slots with no confirmed delivery")
	cmdutil.BindModemFlags(cmd, flags)
	return cmd
}

func runPrune(flags cmdutil.Flags, opts pruneOptions) error {
	base, targets, err := cmdutil.OpenModemTargets(flags)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}
	defer closeModems(targets)

	if base.Storage.Path == "" && !opts.all {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, "error: storage.path is required to tell which messages were forwarded (or pass --all)")
	}

	var store *storage.Store
	if base.Storage.Path != "" {
		store, err = storage.Open(base.Storage.Path)
		if err != nil {
			return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: storage: %v", err))
		}
		defer store.Close()
	}

	multi := len(targets) > 1
	var firstErr error
	for i, target := range targets {
		if i > 0 {
			fmt.Println()
		}
		cmdutil.PrintSection(target.Name, multi)
		if err := pruneOne(target, store, opts); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func pruneOne(target cmdutil.ModemHandle, store *storage.Store, opts pruneOptions) error {
	// Not cmdutil.SMSContext: that budget covers a single query, while a full
	// modem store is 255 slots to list and then delete one AT command at a time.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	del, ok := target.Modem.(modem.SlotDeleter)
	if !ok {
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: driver %s cannot delete from modem storage", target.Config.Driver))
	}
	lister, ok := target.Modem.(modem.PartLister)
	if !ok {
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: driver %s cannot list message segments", target.Config.Driver))
	}

	parts, err := lister.ListMessageParts(ctx)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
	}

	var deleted, kept, failed int
	for _, part := range parts {
		if !modem.IsInbound(part.State) {
			kept++
			continue
		}
		forwarded, err := partForwarded(store, target.Name, part)
		if err != nil {
			return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: storage: %v", err))
		}
		if !forwarded && !opts.all {
			kept++
			continue
		}
		reason := "forwarded"
		if !forwarded {
			reason = "unconfirmed"
		}
		if !opts.apply {
			fmt.Printf("would delete slot=%s from=%s %s\n", part.ID, part.From, reason)
			deleted++
			continue
		}
		delCtx, delCancel := context.WithTimeout(ctx, 15*time.Second)
		err = del.DeleteMessage(delCtx, part.ID)
		delCancel()
		if err != nil {
			fmt.Printf("failed slot=%s from=%s error=%v\n", part.ID, part.From, err)
			failed++
			continue
		}
		fmt.Printf("deleted slot=%s from=%s %s\n", part.ID, part.From, reason)
		deleted++
	}

	verb := "deleted"
	if !opts.apply {
		verb = "would delete"
	}
	fmt.Printf("%s: %d, kept: %d, failed: %d\n", verb, deleted, kept, failed)
	if !opts.apply && deleted > 0 {
		fmt.Println("re-run with --yes to delete")
	}
	if failed > 0 {
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %d slots could not be deleted", failed))
	}
	return nil
}

// partForwarded reports whether the local database shows this segment already
// delivered, either on its own or through the concatenated message it belongs to.
func partForwarded(store *storage.Store, modemName string, part modem.Part) (bool, error) {
	if store == nil {
		return false, nil
	}
	if smsconcat.IsConcat(part) {
		merged, mergedID, err := store.IsConcatMerged(modemName, part.From, part.ConcatRef, part.PartTotal)
		if err != nil {
			return false, err
		}
		if !merged {
			return false, nil
		}
		return store.IsFullyDelivered(mergedID)
	}
	return store.IsFullyDelivered(storage.MessageKey(modemName, part.ID, part.Timestamp))
}
