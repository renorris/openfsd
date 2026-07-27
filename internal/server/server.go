package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/metar"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/pkg/protocol"
)

// Server is the FSD server orchestration layer.
//
// # FSD I/O model
//
// FSD TCP uses the gnet event-driven plane (fixed event-loop count,
// coalesced AsyncWrite outbound). There is no classic net.Listener accept
// path. Tests learn the bound address via Deps.FSDBound (including :0).
// session.SenderWorker remains for sweatbox synthetic sessions (nil Conn).
type Server struct {
	cfg        *Config
	users      UserStore
	configKV   ConfigStore
	registry   Registry
	metar      MetarQueue
	clock      Clock
	logger     *slog.Logger
	fsdBound   chan<- string
	httpListen func(network, addr string) (net.Listener, error)
	// httpDone is closed when runServiceHTTP returns (after Serve exits).
	httpDone chan struct{}
	// sweatbox is the integrated simulator host (nil when disabled).
	sweatbox *SweatboxHost

	// limits tracks concurrent connections and per-CID session counts.
	limits *connLimits
	// authFails rate-limits failed password/JWT logons per IP.
	authFails *authFailLimiter
}

// New constructs a Server from injected Deps.
func New(d Deps) (*Server, error) {
	if d.Config == nil {
		return nil, errors.New("server: Config is required")
	}
	if d.Users == nil {
		return nil, errors.New("server: Users is required")
	}
	if d.ConfigKV == nil {
		return nil, errors.New("server: ConfigKV is required")
	}
	if d.Registry == nil {
		return nil, errors.New("server: Registry is required")
	}
	if d.Metar == nil {
		return nil, errors.New("server: Metar is required")
	}

	clock := d.Clock
	if clock == nil {
		clock = realClock{}
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		cfg:        d.Config,
		users:      d.Users,
		configKV:   d.ConfigKV,
		registry:   d.Registry,
		metar:      d.Metar,
		clock:      clock,
		logger:     logger,
		fsdBound:   d.FSDBound,
		httpListen: d.HTTPListen,
		httpDone:   make(chan struct{}),
		limits:     newConnLimits(),
		authFails:  newAuthFailLimiter(d.Config.AuthFailMax, d.Config.AuthFailWindow),
	}
	// Two-phase: Server exists so SweatboxHost can hold a back-ref for
	// unexported broadcast helpers, registry, clock, and logger.
	if d.SweatboxEnabled {
		s.sweatbox = newSweatboxHost(s)
	}
	return s, nil
}

// NewDefault builds Deps from environment variables and default wiring, then calls New.
func NewDefault(ctx context.Context) (*Server, error) {
	config, err := loadConfig(ctx)
	if err != nil {
		return nil, err
	}

	if err := db.RequireSQLiteDriver(config.DatabaseDriver); err != nil {
		return nil, err
	}

	slog.Info("using sqlite")

	slog.Debug("connecting to SQL")
	sqlDb, err := sql.Open("sqlite", config.DatabaseSourceName)
	if err != nil {
		return nil, err
	}
	slog.Debug("SQL opened")

	if err = sqlDb.PingContext(ctx); err != nil {
		return nil, err
	}

	sqlDb.SetMaxOpenConns(config.DatabaseMaxConns)

	if config.DatabaseAutoMigrate {
		slog.Debug("automatically migrating database")
		if err = db.Migrate(sqlDb); err != nil {
			return nil, err
		}
		slog.Debug("migrate OK")
	}

	dbRepo, err := db.NewRepositories(sqlDb)
	if err != nil {
		return nil, err
	}

	// Generate a default admin user if CID 1 isn't taken
	if _, err = dbRepo.UserRepo.GetUserByCID(1); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		slog.Debug("no user with CID = 1 found, creating default admin user")
		user, genErr := generateDefaultAdminUser(dbRepo)
		if genErr != nil {
			return nil, genErr
		}
		slog.Info(fmt.Sprintf(
			`

	DEFAULT ADMINISTRATOR CREDENTIALS:
	CID:      %d
	Password: %s

`,
			user.CID,
			user.Password,
		))
	}

	// Ensure default configuration is written to persistent storage
	slog.Debug("initializing default config")
	if err = db.InitDefaultConfig(dbRepo.ConfigRepo); err != nil {
		return nil, err
	}
	slog.Debug("config OK")

	metarSvc := metar.New(config.NumMetarWorkers, nil)
	po := postoffice.New()

	return New(Deps{
		Config:          config,
		Users:           dbRepo.UserRepo,
		ConfigKV:        dbRepo.ConfigRepo,
		Registry:        po,
		Metar:           metarSvc,
		SweatboxEnabled: config.SweatboxEnabled,
	})
}

func generateDefaultAdminUser(dbRepo *db.Repositories) (user *db.User, err error) {
	passwordBuf := make([]byte, 8)
	if _, err = io.ReadFull(rand.Reader, passwordBuf); err != nil {
		return
	}
	password := hex.EncodeToString(passwordBuf)

	user = &db.User{
		Password:      password,
		FirstName:     strPtr("Default Administrator"),
		NetworkRating: int(protocol.NetworkRatingAdministator),
	}

	if err = dbRepo.UserRepo.CreateUser(user); err != nil {
		return
	}

	return
}

// Run starts METAR workers, the admin HTTP service, and FSD listeners.
// It blocks until ctx is cancelled (or a listener fails to start).
func (s *Server) Run(ctx context.Context) (err error) {
	// Start metar worker pool (MetarQueue.Run is required on the interface).
	go s.metar.Run(ctx)

	// Sweatbox tick loop (no-op host when disabled / nil).
	if s.sweatbox != nil {
		go s.sweatbox.Run(ctx)
	}

	// Start HTTP service
	go s.runServiceHTTP(ctx)

	err = s.runGnetFSD(ctx)

	// Join service HTTP so callers (and tests) can close shared resources safely.
	select {
	case <-s.httpDone:
	case <-time.After(5 * time.Second):
	}

	return err
}

// runGnetFSD runs the production FSD plane: fixed gnet event loops + coalesced
// AsyncWrite outbound (no 2N connection goroutines).
func (s *Server) runGnetFSD(ctx context.Context) error {
	addrs := s.cfg.FsdListenAddrs
	if len(addrs) == 0 {
		return errors.New("server: no FSD listen addresses")
	}
	for _, addr := range addrs {
		s.logger.Info(fmt.Sprintf("Listening (gnet) on %s\n", addr))
	}

	bound := make(chan string, len(addrs))
	eng := newFSDEngine(s, ctx, addrs, s.cfg.FsdNumEventLoop, bound)

	// Forward bound addresses to Deps.FSDBound if set.
	if s.fsdBound != nil {
		go func() {
			for {
				select {
				case a, ok := <-bound:
					if !ok {
						return
					}
					select {
					case s.fsdBound <- a:
					default:
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- eng.run()
	}()

	// Wait for first bound address or run failure (startup).
	select {
	case a := <-bound:
		// Re-queue for FSDBound forwarder if present.
		select {
		case bound <- a:
		default:
		}
		if s.fsdBound != nil {
			select {
			case s.fsdBound <- a:
			default:
			}
		}
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("gnet FSD failed to start: %w", err)
		}
		return nil
	case <-time.After(5 * time.Second):
		// gnet may not report bound via Dup on all platforms; proceed if still running.
		s.logger.Debug("gnet FSD: no bound address reported within timeout; continuing")
	case <-ctx.Done():
		return ctx.Err()
	}

	// Block until engine exits or context cancelled.
	select {
	case err := <-errCh:
		if err != nil && ctx.Err() == nil {
			return err
		}
		return nil
	case <-ctx.Done():
		// eng.run's cancel watcher stops the engine; wait for exit.
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
		}
		return nil
	}
}

// Compile-time interface satisfaction checks against production implementors.
var (
	_ Registry    = (*postoffice.PostOffice)(nil)
	_ MetarQueue  = (*metar.Service)(nil)
	_ UserStore   = (*db.SQLiteUserRepository)(nil)
	_ UserStore   = db.UserRepository(nil)
	_ ConfigStore = (*db.SQLiteConfigRepository)(nil)
	_ ConfigStore = db.ConfigRepository(nil)
)
