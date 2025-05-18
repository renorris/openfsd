package main

import (
	"context"
	"github.com/sethvargo/go-envconfig"
)

type ServerConfig struct {
	ListenAddr string `env:"LISTEN_ADDR, default=:8000"` // HTTP listen address

	DatabaseDriver     string `env:"DATABASE_DRIVER, default=sqlite"`        // Golang sql database driver name
	DatabaseSourceName string `env:"DATABASE_SOURCE_NAME, default=:memory:"` // Golang sql database source name
	DatabaseMaxConns   int    `env:"DATABASE_MAX_CONNS, default=1"`          // Max number of database connections

	FsdHttpServiceAddress string `env:"FSD_HTTP_SERVICE_ADDRESS, required"` // HTTP address to talk to the FSD http service
}

func loadServerConfig(ctx context.Context) (config *ServerConfig, err error) {
	config = &ServerConfig{}
	if err = envconfig.Process(ctx, config); err != nil {
		return
	}
	return
}
