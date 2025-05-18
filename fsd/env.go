package fsd

import (
	"context"
	"github.com/sethvargo/go-envconfig"
)

type ServerConfig struct {
	FsdListenAddrs []string `env:"FSD_LISTEN_ADDRS, default=:6809"` // FSD listen addresses

	DatabaseDriver      string `env:"DATABASE_DRIVER, default=sqlite"`        // Golang sql database driver name
	DatabaseSourceName  string `env:"DATABASE_SOURCE_NAME, default=:memory:"` // Golang sql database source name
	DatabaseAutoMigrate bool   `env:"DATABASE_AUTO_MIGRATE, default=false"`   // Whether to automatically run database migrations on startup
	DatabaseMaxConns    int    `env:"DATABASE_MAX_CONNS, default=1"`          // Max number of database connections

	NumMetarWorkers int `env:"NUM_METAR_WORKERS, default=4"` // Number of METAR fetch workers to run

	ServiceHTTPListenAddr string `env:"SERVICE_HTTP_LISTEN_ADDR, default=:13618"`
}

func loadServerConfig(ctx context.Context) (config *ServerConfig, err error) {
	config = &ServerConfig{}
	if err = envconfig.Process(ctx, config); err != nil {
		return
	}
	return
}
