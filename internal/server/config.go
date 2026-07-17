package server

import (
	"context"

	"github.com/sethvargo/go-envconfig"
)

// Config is envconfig-based FSD server configuration.
type Config struct {
	FsdListenAddrs []string `env:"FSD_LISTEN_ADDRS, default=:6809"` // FSD listen addresses

	DatabaseDriver      string `env:"DATABASE_DRIVER, default=sqlite"`        // Golang sql database driver name
	DatabaseSourceName  string `env:"DATABASE_SOURCE_NAME, default=:memory:"` // Golang sql database source name
	DatabaseAutoMigrate bool   `env:"DATABASE_AUTO_MIGRATE, default=true"`    // Whether to automatically run database migrations on startup
	DatabaseMaxConns    int    `env:"DATABASE_MAX_CONNS, default=1"`          // Max number of database connections

	NumMetarWorkers int `env:"NUM_METAR_WORKERS, default=4"` // Number of METAR fetch workers to run

	ServiceHTTPListenAddr string `env:"SERVICE_HTTP_LISTEN_ADDR, default=:13618"`
}

func loadConfig(ctx context.Context) (*Config, error) {
	config := &Config{}
	if err := envconfig.Process(ctx, config); err != nil {
		return nil, err
	}
	return config, nil
}
