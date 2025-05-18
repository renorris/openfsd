package main

import (
	"context"
	"github.com/renorris/openfsd/fsd"
	"log/slog"
	_ "modernc.org/sqlite"
	"os"
	"os/signal"
)

func main() {
	setSlogLevel()

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)

	os.Setenv("DATABASE_AUTO_MIGRATE", "true")
	os.Setenv("DATABASE_DRIVER", "sqlite")
	os.Setenv("DATABASE_SOURCE_NAME", "test.db")

	server, err := fsd.NewDefaultServer(ctx)
	if err != nil {
		panic(err)
	}

	if err = server.Run(ctx); err != nil {
		slog.Error(err.Error())
	}
	slog.Info("server closed")
}

func setSlogLevel() {
	if os.Getenv("LOG_DEBUG") == "true" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
}
