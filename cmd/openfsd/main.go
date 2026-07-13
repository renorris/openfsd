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

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
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
