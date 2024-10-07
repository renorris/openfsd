package main

import (
	"context"
	"github.com/renorris/openfsd/bootstrap"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	b := bootstrap.NewDefaultBootstrap()

	log.Println("Starting services...")

	ctx, cancel := context.WithCancel(context.Background())
	if err := b.Start(ctx); err != nil {
		log.Fatalln(err)
	}

	log.Println("All services started.")

	// Listen for OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either an error or a shutdown signal
	select {
	case sig := <-sigCh:
		log.Printf("received %s", sig)
		cancel()
	case <-b.Done:
		log.Println("FATAL: a service exited early.")
	}

	log.Println("Waiting for services to stop...")
	<-b.Done
	log.Println("All services stopped.")
}
