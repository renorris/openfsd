package web

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

	// CookieSecure controls the Secure attribute on session/CSRF cookies.
	// Values: "true"/"false" force the flag; empty (default) derives from
	// TLS / X-Forwarded-Proto so local docker-compose HTTP keeps working.
	CookieSecure string `env:"COOKIE_SECURE"`
}

func loadServerConfig(ctx context.Context) (config *ServerConfig, err error) {
	config = &ServerConfig{}
	if err = envconfig.Process(ctx, config); err != nil {
		return
	}
	return
}
