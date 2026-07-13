package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/renorris/openfsd/internal/web"
)

func main() {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
	if err := web.Main(ctx); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
