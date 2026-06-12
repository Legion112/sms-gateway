package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/forward"
	"github.com/legion/sms-gateway/internal/storage"
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

	router := forward.NewRouter(cfg.ForwardRules)
	svc := watch.NewService(cfg, watch.Dependencies{
		Store:    store,
		Router:   router,
		Channels: channels,
		Log:      log.Printf,
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

func closeChannels(channels map[string]forward.Channel) {
	for name, ch := range channels {
		if err := ch.Close(); err != nil {
			log.Printf("close channel %s: %v", name, err)
		}
	}
}
