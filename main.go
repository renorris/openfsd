package main

import (
	"context"
	"fmt"
	"github.com/renorris/openfsd/fsd"
	"log/slog"
	_ "modernc.org/sqlite"
	"os"
	"os/signal"
)

func main() {
	fmt.Println("hello world")

	setSlogLevel()

	os.Setenv("DATABASE_AUTO_MIGRATE", "true")
	server, err := fsd.NewDefaultServer(context.Background())
	if err != nil {
		panic(err)
	}

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
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
