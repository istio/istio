package main

import (
	"context"
	"istio.io/istio/centralized/cmd/acmg/app"
	"os"
	"os/signal"
	"syscall"

	"istio.io/pkg/log"
)

func main() {
	// Create context that cancels on termination signal
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func(sigChan chan os.Signal, cancel context.CancelFunc) {
		sig := <-sigChan
		log.Infof("Exit signal received: %s", sig)
		cancel()
	}(sigChan, cancel)

	rootCmd := app.GetCommand()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
