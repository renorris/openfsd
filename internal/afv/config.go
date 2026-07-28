package afv

import (
	"context"
	"time"

	"github.com/sethvargo/go-envconfig"
)

// Config is envconfig-based AFV server configuration (AFV_* + shared DATABASE_*).
type Config struct {
	APIListen             string        `env:"AFV_API_LISTEN, default=127.0.0.1:8080"`
	APIPublicBaseURL      string        `env:"AFV_API_PUBLIC_BASE_URL"`
	UDPListen             string        `env:"AFV_UDP_LISTEN, default=0.0.0.0:50000"`
	UDPAdvertiseIPv4      string        `env:"AFV_UDP_ADVERTISE_IPV4"` // required when starting UDP
	UDPAdvertiseIPv6      string        `env:"AFV_UDP_ADVERTISE_IPV6"`
	TLSCertFile           string        `env:"AFV_TLS_CERT_FILE"`
	TLSKeyFile            string        `env:"AFV_TLS_KEY_FILE"`
	JWTSecret             string        `env:"AFV_JWT_SECRET"` // override; else config KV
	JWTTTL                time.Duration `env:"AFV_JWT_TTL, default=1h"`
	HeartbeatTimeout      time.Duration `env:"AFV_HEARTBEAT_TIMEOUT, default=10s"`
	SessionIdleTimeout    time.Duration `env:"AFV_SESSION_IDLE_TIMEOUT, default=30s"`
	MaxSessions           int           `env:"AFV_MAX_SESSIONS, default=5000"`
	MaxSessionsPerCID     int           `env:"AFV_MAX_SESSIONS_PER_CID, default=5"`
	AuthFailMax           int           `env:"AFV_AUTH_FAIL_MAX, default=20"`
	AuthFailWindow        time.Duration `env:"AFV_AUTH_FAIL_WINDOW, default=1m"`
	MaxDatagram           int           `env:"AFV_MAX_DATAGRAM, default=8192"`
	RequireFSDOnline      bool          `env:"AFV_REQUIRE_FSD_ONLINE, default=false"`
	FSDHTTPServiceAddress string        `env:"AFV_FSD_HTTP_SERVICE_ADDRESS, default=http://127.0.0.1:13618"`
	CallsignStrict        bool          `env:"AFV_CALLSIGN_STRICT, default=false"`
	RangeUnicomNM         float64       `env:"AFV_RANGE_UNICOM_NM, default=15"`
	RangeDefaultNM        float64       `env:"AFV_RANGE_DEFAULT_NM, default=40"`
	RangeATCNM            float64       `env:"AFV_RANGE_ATC_NM, default=150"`
	RangeEdgeRatio        float64       `env:"AFV_RANGE_EDGE_RATIO, default=0.1"`
	StationsFile          string        `env:"AFV_STATIONS_FILE"`
	CrossCouple           bool          `env:"AFV_CROSS_COUPLE, default=true"`
	ClusterEnabled        bool          `env:"AFV_CLUSTER_ENABLED, default=false"`
	ClusterNodeID         string        `env:"AFV_CLUSTER_NODE_ID"`
	ClusterListen         string        `env:"AFV_CLUSTER_LISTEN"`
	ClusterVoiceListen    string        `env:"AFV_CLUSTER_VOICE_LISTEN"`
	ClusterPeers          string        `env:"AFV_CLUSTER_PEERS"`
	ClusterPSK            string        `env:"AFV_CLUSTER_PSK"`

	// Shared database env (same names as FSD/web).
	DatabaseDriver        string `env:"DATABASE_DRIVER, default=sqlite"`
	DatabaseSourceName    string `env:"DATABASE_SOURCE_NAME, default=openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"`
	DatabaseAutoMigrate   bool   `env:"DATABASE_AUTO_MIGRATE, default=true"`
	DatabaseMigrateLeader bool   `env:"DATABASE_MIGRATE_LEADER, default=false"`
	DatabaseMaxConns      int    `env:"DATABASE_MAX_CONNS, default=1"`
	AuthReadLevel         string `env:"AUTH_READ_LEVEL, default=weak"`
}

func loadConfig(ctx context.Context) (*Config, error) {
	cfg := &Config{}
	if err := envconfig.Process(ctx, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
