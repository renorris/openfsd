package main

import (
	"context"
	"github.com/renorris/openfsd/fsd"
	"log/slog"
	"os"
	"os/signal"
)

func main() {
	setSlogLevel()

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
	server, err := fsd.NewDefaultServer(ctx)
	if err != nil {
		panic(err)
	}

	if err = server.Run(ctx); err != nil {
		slog.Error(err.Error())
	}
	slog.Info("FSD server closed")
}

func setSlogLevel() {
	if os.Getenv("LOG_DEBUG") == "true" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
}
