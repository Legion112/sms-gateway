package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/translate"
	"github.com/spf13/cobra"
)

func newTranslateCmd(root *cmdutil.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "translate [text]",
		Short: "Translate text through the configured engine",
		Long: "Translate text through the configured local engine.\n\n" +
			"This is how the engine gets tested without waiting for an SMS to arrive:\n" +
			"it reports which language the text was read as, whether that is a language\n" +
			"the gateway would translate, and what the engine answered.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranslate(root, strings.Join(args, " "))
		},
	}
	return cmd
}

func runTranslate(root *cmdutil.Flags, text string) error {
	cfg, err := cmdutil.LoadConfig(*root)
	if err != nil {
		return err
	}
	if !cfg.Translate.Enabled {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, "error: translation is not enabled (set translate.enabled: true)")
	}
	if err := config.ValidateTranslate(cfg); err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}

	// The detection is printed before the engine is called, because a message
	// the gateway would leave alone is the most common surprise: half of this
	// traffic is Russian in Latin letters, which nothing here translates.
	detected := translate.Detect(text)
	fmt.Printf("detected: %s\n", describeLanguage(detected))

	sources := cfg.TranslateSources()
	target := cfg.TranslateTarget()
	fmt.Printf("would translate: %v -> %s\n", sources, target)
	if !contains(sources, detected) {
		fmt.Printf("\nverdict: forwarded unchanged, %s is not a language this gateway translates\n", describeLanguage(detected))
		return nil
	}

	engine, err := translate.NewEngine(cfg)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}
	fmt.Printf("engine: %s at %s\n", engine.Name(), cfg.Translate.Endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.TranslateTimeout()+5*time.Second)
	defer cancel()
	start := time.Now()
	out, err := engine.Translate(ctx, text, detected, target)
	if err != nil {
		return cmdutil.NewCLIError(cmdutil.ExitDependency, fmt.Sprintf("error: %v", err))
	}
	fmt.Printf("took: %s\n\n%s\n", time.Since(start).Truncate(time.Millisecond), out)
	return nil
}

func describeLanguage(tag string) string {
	switch tag {
	case translate.Uzbek:
		return "uz (Uzbek)"
	case translate.Russian:
		return "ru (Russian)"
	case translate.English:
		return "en (English)"
	default:
		return "undecided"
	}
}

func contains(list []string, want string) bool {
	if want == "" {
		return false
	}
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
