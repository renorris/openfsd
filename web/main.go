package main

import (
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)

	os.Setenv("DATABASE_DRIVER", "sqlite")
	os.Setenv("DATABASE_SOURCE_NAME", "../test.db")
	os.Setenv("FSD_HTTP_SERVICE_ADDRESS", "http://localhost:13618")

	server, err := NewDefaultServer(ctx)
	if err != nil {
		panic(err)
	}

	server.Run(ctx)
}
