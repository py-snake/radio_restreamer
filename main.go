package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"sync"
	"syscall"
)

func main() {
	configPath := flag.String("config", "config.json", "path to JSON config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	log.Printf("loaded %d stream(s), admin port %d", len(cfg.Streams), cfg.AdminPort)

	managers := make([]*StreamManager, 0, len(cfg.Streams))
	for _, sc := range cfg.Streams {
		sm, err := NewStreamManager(sc)
		if err != nil {
			log.Fatalf("stream init error: %v", err)
		}
		managers = append(managers, sm)
	}

	// Root context — cancelled on SIGINT or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	for _, sm := range managers {
		wg.Add(1)
		sm := sm
		go func() {
			defer wg.Done()
			sm.Run(ctx)
		}()
	}

	admin := NewAdminServer(cfg.AdminPort, managers)
	go admin.Run(ctx)

	<-ctx.Done()
	log.Printf("shutdown signal received, draining...")
	wg.Wait()
	log.Printf("shutdown complete")
}
