package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/legion/sms-gateway/internal/alert"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/storage"
	"github.com/legion/sms-gateway/internal/translate"
	"github.com/legion/sms-gateway/internal/watch"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "", "config file path")
	verbose := flag.Bool("v", false, "verbose logging")
	flag.Parse()

	cfg, err := config.Load(config.Overrides{
		ConfigPath: *configPath,
		Verbose:    *verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}
	if err := config.ValidateWatch(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}

	store, err := storage.Open(cfg.Storage.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storage: %v\n", err)
		return 2
	}
	defer store.Close()

	channels, err := forward.NewChannels(cfg.Channels, cfg.Verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "channels: %v\n", err)
		return 2
	}
	defer closeChannels(channels)

	translator, err := translate.FromConfig(cfg, log.Printf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "translate: %v\n", err)
		return 2
	}
	if translator == nil {
		log.Printf("translate: off, messages are forwarded as received")
	} else {
		log.Printf("translate: %v -> %s via %s", cfg.TranslateSources(), translator.Target(), cfg.Translate.Endpoint)
	}

	router := forward.NewRouter(cfg.ForwardRules)
	svc := watch.NewService(cfg, watch.Dependencies{
		Store:    store,
		Router:   router,
		Channels: channels,
		Log:      log.Printf,
		Alerts:   newAlertDispatcher(cfg, channels),

		Translator: translator,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("sms-gateway-watch: started (%d modems, db=%s)", len(cfg.Modems), cfg.Storage.Path)
	err = svc.Run(ctx)
	if err != nil && err != context.Canceled {
		log.Printf("sms-gateway-watch: %v", err)
		return 1
	}
	log.Printf("sms-gateway-watch: stopped")
	return 0
}

// newAlertDispatcher wires operational alerts to the configured channels, or
// returns nil when none are configured, which switches alerting off.
//
// Everything is one binary today, so an alert leaves through the same Telegram
// channel that carries forwarded SMS. When a separate notification service
// arrives, only the Notifier passed here changes.
func newAlertDispatcher(cfg config.Config, channels map[string]forward.Channel) *alert.Dispatcher {
	if !cfg.AlertsEnabled() {
		log.Printf("alerts: no channels configured, modem outages will only be logged")
		return nil
	}
	senders := make([]alert.TextSender, 0, len(cfg.Alerts.Channels))
	for _, name := range cfg.Alerts.Channels {
		ch, ok := channels[name]
		if !ok {
			// ValidateAlerts rejects this at load, so reaching here means the
			// channel failed to build; say so rather than alerting into a void.
			log.Printf("alerts: channel %s unavailable", name)
			continue
		}
		senders = append(senders, ch)
	}
	d := alert.NewDispatcher(alert.NewChannelNotifier(senders...), log.Printf)
	if d == nil {
		log.Printf("alerts: no usable channels, modem outages will only be logged")
		return nil
	}
	log.Printf("alerts: modem silence over %s reported to %v", cfg.AlertAfter(), cfg.Alerts.Channels)
	return d
}

func closeChannels(channels map[string]forward.Channel) {
	for name, ch := range channels {
		if err := ch.Close(); err != nil {
			log.Printf("close channel %s: %v", name, err)
		}
	}
}
