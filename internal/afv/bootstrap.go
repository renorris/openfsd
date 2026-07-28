package afv

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/renorris/openfsd/internal/db"
)

// NewDefault loads envconfig, opens DB, migrates, seeds config, resolves JWT secret.
// Works with -afv alone (empty DB) and colocated with FSD/web.
func NewDefault(ctx context.Context) (*Server, error) {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("afv config: %w", err)
	}
	if err := db.RequireDatabaseDriver(cfg.DatabaseDriver); err != nil {
		return nil, err
	}
	driver := cfg.DatabaseDriver
	if driver == "" {
		driver = "sqlite"
	}
	slog.Info("AFV using database driver", "driver", driver)

	readLevel, err := db.ParseReadLevel(cfg.AuthReadLevel)
	if err != nil {
		return nil, err
	}

	if cfg.DatabaseAutoMigrate && (driver == "sqlite" || driver == "") {
		sqlDB, errOpen := sql.Open("sqlite", cfg.DatabaseSourceName)
		if errOpen != nil {
			return nil, errOpen
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		sqlDB.SetMaxOpenConns(cfg.DatabaseMaxConns)
		if err := db.Migrate(sqlDB); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		_ = sqlDB.Close()
		slog.Debug("AFV migrate OK")
	} else if cfg.DatabaseAutoMigrate && driver == "rqlite" && cfg.DatabaseMigrateLeader {
		client := db.NewRqliteClient(cfg.DatabaseSourceName, db.RqliteClientOptions{})
		if err := db.MigrateRqlite(ctx, client); err != nil {
			return nil, err
		}
	}

	repos, err := db.OpenRepositories(ctx, driver, cfg.DatabaseSourceName, readLevel, true)
	if err != nil {
		return nil, err
	}
	if err := db.InitDefaultConfig(ctx, repos.ConfigRepo); err != nil {
		return nil, err
	}

	jwtSecret, err := resolveJWTSecret(ctx, cfg, repos.ConfigRepo)
	if err != nil {
		return nil, err
	}
	if cfg.UDPAdvertiseIPv4 == "" {
		return nil, fmt.Errorf("AFV_UDP_ADVERTISE_IPV4 is required when starting AFV")
	}
	// P0: FSD online gate is not wired yet (PR-7). Refuse to start if operators
	// enable the flag so it cannot silently imply protection that is absent.
	if cfg.RequireFSDOnline {
		return nil, fmt.Errorf("AFV_REQUIRE_FSD_ONLINE=true is not implemented in P0; leave false until the FSD online gate lands")
	}

	if err := cfg.ValidateCluster(); err != nil {
		return nil, err
	}
	// M-11: ENABLED=true without TCP mesh in this binary fails closed.
	// Never wire production NewDefault to MemoryMesh.
	if cfg.ClusterEnabled {
		return nil, errClusterTCPNotBuilt
	}

	return New(cfg, repos.UserRepo, repos.ConfigRepo, jwtSecret), nil
}

func resolveJWTSecret(ctx context.Context, cfg *Config, kv db.ConfigRepository) ([]byte, error) {
	if cfg.JWTSecret != "" {
		return []byte(cfg.JWTSecret), nil
	}
	v, err := kv.Get(ctx, db.ConfigJwtSecretKey)
	if err != nil {
		return nil, fmt.Errorf("JWT secret: %w", err)
	}
	if v == "" {
		return nil, fmt.Errorf("JWT secret empty")
	}
	return []byte(v), nil
}
