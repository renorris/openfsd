package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/renorris/openfsd/internal/server"
)

func main() {
	setSlogLevel()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	srv, err := server.NewDefault(ctx)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	if err = srv.Run(ctx); err != nil {
		slog.Error(err.Error())
	}
	slog.Info("FSD server closed")
}

func setSlogLevel() {
	if os.Getenv("LOG_DEBUG") == "true" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
}
