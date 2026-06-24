package cli

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/driver/mm"
	"github.com/spf13/cobra"
)

func newModemsCmd(flags *cmdutil.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "modems",
		Short: "List configured and discovered modems",
	}
	cmd.AddCommand(newModemsListCmd(flags))
	return cmd
}

func newModemsListCmd(flags *cmdutil.Flags) *cobra.Command {
	var discover bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List modems from config and optionally ModemManager",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModemsList(*flags, discover)
		},
	}
	cmd.Flags().BoolVar(&discover, "discover", false, "also list modems discovered via ModemManager")
	return cmd
}

func runModemsList(flags cmdutil.Flags, discover bool) error {
	cfg, err := cmdutil.LoadConfig(flags)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}

	if len(cfg.Modems) == 0 {
		fmt.Println("configured: (none — using legacy top-level driver/mm/serial settings)")
	} else {
		names := make([]string, 0, len(cfg.Modems))
		for name := range cfg.Modems {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Println("configured:")
		for _, name := range names {
			printConfiguredModem(name, cfg.Modems[name], cfg.DefaultModem == name)
		}
	}

	if !discover {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	modems, err := mm.ListModems(ctx, cfg.MM.Timeout)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("discover: %v", err))
	}
	if len(modems) == 0 {
		fmt.Println("discovered: (none)")
		return nil
	}
	fmt.Println("discovered:")
	for _, m := range modems {
		fmt.Printf("  index: %d\n", m.Index)
		fmt.Printf("  path: %s\n", m.Path)
		if m.Manufacturer != "" {
			fmt.Printf("  manufacturer: %s\n", m.Manufacturer)
		}
		if m.Model != "" {
			fmt.Printf("  model: %s\n", m.Model)
		}
		fmt.Println("  ---")
	}
	return nil
}

func printConfiguredModem(name string, entry config.ModemEntry, isDefault bool) {
	fmt.Printf("  name: %s\n", name)
	if isDefault {
		fmt.Println("  default: true")
	}
	fmt.Printf("  driver: %s\n", entry.Driver)
	if entry.Driver == config.DriverMM {
		fmt.Printf("  modem_index: %d\n", entry.MM.ModemIndex)
		if entry.MM.ModemPath != "" {
			fmt.Printf("  modem_path: %s\n", entry.MM.ModemPath)
		}
	}
	if entry.Driver == config.DriverSerial {
		fmt.Printf("  device: %s\n", entry.Serial.Device)
	}
	fmt.Println("  ---")
}
