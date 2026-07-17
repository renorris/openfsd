package web

import (
	"context"

	"github.com/sethvargo/go-envconfig"
)

type ServerConfig struct {
	ListenAddr string `env:"LISTEN_ADDR, default=:8000"` // HTTP listen address

	// DatabaseDriver is accepted for backward compatibility only.
	// openfsd is SQLite-only; any non-empty value other than "sqlite" is rejected at startup.
	DatabaseDriver     string `env:"DATABASE_DRIVER, default=sqlite"`
	DatabaseSourceName string `env:"DATABASE_SOURCE_NAME, default=:memory:"` // SQLite DSN (path or :memory:)
	DatabaseMaxConns   int    `env:"DATABASE_MAX_CONNS, default=1"`          // Max number of database connections

	// FsdHttpServiceAddress is the base URL of the FSD internal service HTTP API.
	// Default assumes colocated FSD in the same process (or host). Override when
	// running -web against a remote FSD instance.
	FsdHttpServiceAddress string `env:"FSD_HTTP_SERVICE_ADDRESS, default=http://127.0.0.1:13618"`

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
