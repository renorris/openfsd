package web

import (
	"context"

	"github.com/sethvargo/go-envconfig"
)

type ServerConfig struct {
	ListenAddr string `env:"LISTEN_ADDR, default=:8000"` // HTTP listen address

	// DatabaseDriver selects sqlite (default) or rqlite.
	DatabaseDriver string `env:"DATABASE_DRIVER, default=sqlite"`
	// DatabaseSourceName is the SQLite DSN or rqlite HTTP base URL.
	// Must match FSD when colocated so both share users/config.
	DatabaseSourceName  string `env:"DATABASE_SOURCE_NAME, default=openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"`
	DatabaseAutoMigrate bool   `env:"DATABASE_AUTO_MIGRATE, default=true"` // Run migrations on startup (safe if FSD already migrated)
	// DatabaseMigrateLeader allows this process to apply rqlite migrations.
	DatabaseMigrateLeader bool `env:"DATABASE_MIGRATE_LEADER, default=false"`
	DatabaseMaxConns      int  `env:"DATABASE_MAX_CONNS, default=1"` // Max number of database connections

	// AuthReadLevel for rqlite user reads (weak|strong|none).
	AuthReadLevel string `env:"AUTH_READ_LEVEL, default=weak"`

	// FsdHttpServiceAddress is the base URL of the FSD internal service HTTP API.
	// Default assumes colocated FSD in the same process (or host). Override when
	// running -web against a remote FSD instance.
	FsdHttpServiceAddress string `env:"FSD_HTTP_SERVICE_ADDRESS, default=http://127.0.0.1:13618"`
	// FsdHttpServiceAddresses is a comma-separated multi-FSD list (PR-9).
	// When set, online_users is aggregated and kick routes by node_id.
	// Falls back to FsdHttpServiceAddress when empty.
	FsdHttpServiceAddresses string `env:"FSD_HTTP_SERVICE_ADDRESSES"`

	// CookieSecure controls the Secure attribute on session/CSRF cookies.
	// Values: "true"/"false" force the flag; empty (default) derives from
	// TLS / X-Forwarded-Proto so local docker-compose HTTP keeps working.
	CookieSecure string `env:"COOKIE_SECURE"`

	// AllowPermanentAccountDelete enables the non-default hard-delete checkbox
	// on POST /account/delete. Default false (soft-delete only).
	AllowPermanentAccountDelete bool `env:"ALLOW_PERMANENT_ACCOUNT_DELETE, default=false"`
}

func loadServerConfig(ctx context.Context) (config *ServerConfig, err error) {
	config = &ServerConfig{}
	if err = envconfig.Process(ctx, config); err != nil {
		return
	}
	return
}
