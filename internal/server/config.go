package server

import (
	"context"
	"time"

	"github.com/sethvargo/go-envconfig"
)

// Config is envconfig-based FSD server configuration.
//
// Security/limit fields: zero means unlimited / disabled (useful for tests that
// construct Config{} manually). NewDefault loads envconfig defaults for production.
type Config struct {
	FsdListenAddrs []string `env:"FSD_LISTEN_ADDRS, default=:6809"` // FSD listen addresses

	// FsdNumEventLoop is the number of gnet event-loop goroutines for the FSD
	// TCP plane. 0 (default) means GOMAXPROCS.
	FsdNumEventLoop int `env:"FSD_NUM_EVENT_LOOP, default=0"`

	// DatabaseDriver is accepted for backward compatibility only.
	// openfsd is SQLite-only; any non-empty value other than "sqlite" is rejected at startup.
	DatabaseDriver string `env:"DATABASE_DRIVER, default=sqlite"`
	// DatabaseSourceName is the SQLite DSN. Default is a local file with WAL so
	// colocated FSD+web share one database. Bare ":memory:" is private per
	// sql.Open — cmd/openfsd rewrites it to a shared in-memory DSN when both
	// services run. See db.DefaultSQLiteDSN / db.SharedMemorySQLiteDSN.
	DatabaseSourceName  string `env:"DATABASE_SOURCE_NAME, default=openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"`
	DatabaseAutoMigrate bool   `env:"DATABASE_AUTO_MIGRATE, default=true"` // Whether to automatically run database migrations on startup
	DatabaseMaxConns    int    `env:"DATABASE_MAX_CONNS, default=1"`       // Max number of database connections

	NumMetarWorkers int `env:"NUM_METAR_WORKERS, default=4"` // Number of METAR fetch workers to run

	// ServiceHTTPListenAddr is the admin/control-plane HTTP bind address.
	// Default loopback — the service API must not be exposed publicly.
	ServiceHTTPListenAddr string `env:"SERVICE_HTTP_LISTEN_ADDR, default=127.0.0.1:13618"`

	// Connection / session limits (0 = unlimited).
	FsdMaxConnections      int `env:"FSD_MAX_CONNECTIONS, default=5000"`
	FsdMaxConnectionsPerIP int `env:"FSD_MAX_CONNECTIONS_PER_IP, default=50"`
	FsdMaxSessionsPerCID   int `env:"FSD_MAX_SESSIONS_PER_CID, default=5"`

	// Timeouts (0 = disabled). Applied on the gnet FSD plane.
	FsdLoginTimeout time.Duration `env:"FSD_LOGIN_TIMEOUT, default=30s"`
	FsdIdleTimeout  time.Duration `env:"FSD_IDLE_TIMEOUT, default=120s"`

	// FsdMaxAtcVisRangeNM caps ATC % visibility range (nautical miles).
	// Always enforced; if 0 or negative at runtime, 1500 is used.
	FsdMaxAtcVisRangeNM float64 `env:"FSD_MAX_ATC_VIS_RANGE_NM, default=1500"`

	// AuthFailMax / AuthFailWindow rate-limit failed password logons per IP.
	// AuthFailMax 0 disables. Defaults: 20 failures per minute per IP.
	AuthFailMax    int           `env:"FSD_AUTH_FAIL_MAX, default=20"`
	AuthFailWindow time.Duration `env:"FSD_AUTH_FAIL_WINDOW, default=1m"`

	// FsdEnableRateLimits enables per-session message rate limits (position, text, METAR, FPL).
	// Production default true via envconfig; hand-built test Config{} leaves this false.
	FsdEnableRateLimits bool `env:"FSD_ENABLE_RATE_LIMITS, default=true"`

	// Sweatbox (integrated simulator). Disabled by default for general FSD deploys.
	SweatboxEnabled      bool          `env:"SWEATBOX_ENABLED, default=false"`
	SweatboxCID          int           `env:"SWEATBOX_CID, default=900001"`
	SweatboxTickInterval time.Duration `env:"SWEATBOX_TICK_INTERVAL, default=1s"`
	// SweatboxTickHz, when > 0, overrides SweatboxTickInterval (interval = 1/Hz).
	SweatboxTickHz float64 `env:"SWEATBOX_TICK_HZ"`
}

// maxAtcVisRangeNM returns the ATC visibility range cap in nautical miles.
func (c *Config) maxAtcVisRangeNM() float64 {
	if c == nil || c.FsdMaxAtcVisRangeNM <= 0 {
		return 1500
	}
	return c.FsdMaxAtcVisRangeNM
}

func loadConfig(ctx context.Context) (*Config, error) {
	config := &Config{}
	if err := envconfig.Process(ctx, config); err != nil {
		return nil, err
	}
	return config, nil
}
