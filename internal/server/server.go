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
	"strings"
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/cluster"
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

	// mesh is optional cluster fabric (nil when CLUSTER_ENABLED=false).
	mesh cluster.Mesh
	// authPool runs login auth + ClaimReserve + HomeRPC off the gnet loop.
	authPool   chan func()
	authPoolWG sync.WaitGroup
	// nodeID for online_users / datafeed (empty when single-node).
	nodeID string
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

	reg := d.Registry
	if d.Mesh != nil {
		if _, ok := reg.(*HybridRegistry); !ok {
			reg = NewHybridRegistry(reg, d.Mesh)
		}
	}

	s := &Server{
		cfg:        d.Config,
		users:      d.Users,
		configKV:   d.ConfigKV,
		registry:   reg,
		metar:      d.Metar,
		clock:      clock,
		logger:     logger,
		fsdBound:   d.FSDBound,
		httpListen: d.HTTPListen,
		httpDone:   make(chan struct{}),
		limits:     newConnLimits(),
		authFails:  newAuthFailLimiter(d.Config.AuthFailMax, d.Config.AuthFailWindow),
		mesh:       d.Mesh,
		authPool:   make(chan func(), 256),
		nodeID:     d.Config.ClusterNodeID,
	}
	// Auth/HomeRPC worker pool (off gnet loop). Closed in shutdownCluster.
	s.authPoolWG.Add(4)
	for i := 0; i < 4; i++ {
		go func() {
			defer s.authPoolWG.Done()
			for fn := range s.authPool {
				if fn != nil {
					fn()
				}
			}
		}()
	}
	if d.Mesh != nil {
		s.wireMeshHandlers()
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

	if err := db.RequireDatabaseDriver(config.DatabaseDriver); err != nil {
		return nil, err
	}

	driver := config.DatabaseDriver
	if driver == "" {
		driver = "sqlite"
	}
	slog.Info("using database driver", "driver", driver)

	readLevel, err := db.ParseReadLevel(config.AuthReadLevel)
	if err != nil {
		return nil, err
	}

	// Migrate before opening cached repos when leader.
	if config.DatabaseAutoMigrate {
		if err := migrateDatabase(ctx, config); err != nil {
			return nil, err
		}
	}

	dbRepo, err := db.OpenRepositories(ctx, driver, config.DatabaseSourceName, readLevel, true)
	if err != nil {
		return nil, err
	}

	// Generate a default admin user if CID 1 isn't taken
	if _, err = dbRepo.UserRepo.GetUserByCID(ctx, 1); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		slog.Debug("no user with CID = 1 found, creating default admin user")
		user, genErr := generateDefaultAdminUser(ctx, dbRepo)
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
	if err = db.InitDefaultConfig(ctx, dbRepo.ConfigRepo); err != nil {
		return nil, err
	}
	slog.Debug("config OK")

	metarSvc := metar.New(config.NumMetarWorkers, nil)
	po := postoffice.New()

	var mesh cluster.Mesh
	if config.ClusterEnabled {
		mesh, err = buildClusterMesh(config)
		if err != nil {
			return nil, err
		}
		if err = mesh.Start(ctx); err != nil {
			return nil, fmt.Errorf("cluster mesh start: %w", err)
		}
	}

	return New(Deps{
		Config:          config,
		Users:           dbRepo.UserRepo,
		ConfigKV:        dbRepo.ConfigRepo,
		Registry:        po,
		Metar:           metarSvc,
		SweatboxEnabled: config.SweatboxEnabled,
		Mesh:            mesh,
	})
}

func migrateDatabase(ctx context.Context, config *Config) error {
	driver := config.DatabaseDriver
	if driver == "" {
		driver = "sqlite"
	}
	switch driver {
	case "sqlite":
		slog.Debug("automatically migrating database")
		sqlDb, err := sql.Open("sqlite", config.DatabaseSourceName)
		if err != nil {
			return err
		}
		defer sqlDb.Close()
		if err = sqlDb.PingContext(ctx); err != nil {
			return err
		}
		if err = db.Migrate(sqlDb); err != nil {
			return err
		}
		slog.Debug("migrate OK")
		return nil
	case "rqlite":
		if !config.DatabaseMigrateLeader {
			slog.Debug("skipping rqlite migrate (not migrate leader)")
			return nil
		}
		client := db.NewRqliteClient(config.DatabaseSourceName, db.RqliteClientOptions{})
		if err := db.MigrateRqlite(ctx, client); err != nil {
			return err
		}
		slog.Debug("rqlite migrate OK")
		return nil
	default:
		return fmt.Errorf("unknown driver %q", driver)
	}
}

func buildClusterMesh(config *Config) (cluster.Mesh, error) {
	if config.ClusterNodeID == "" {
		return nil, errors.New("CLUSTER_NODE_ID required when CLUSTER_ENABLED")
	}
	if config.ClusterListen == "" {
		return nil, errors.New("CLUSTER_LISTEN required when CLUSTER_ENABLED")
	}
	peers, err := cluster.ParseClusterPeers(config.ClusterPeers)
	if err != nil {
		return nil, err
	}
	// Ensure self is in peer list for ring
	hasSelf := false
	for _, p := range peers {
		if p.NodeID == config.ClusterNodeID {
			hasSelf = true
			break
		}
	}
	if !hasSelf {
		peers = append(peers, cluster.PeerAddr{NodeID: config.ClusterNodeID, Addr: config.ClusterListen})
	}
	if strings.TrimSpace(config.ClusterPSK) == "" {
		return nil, errors.New("CLUSTER_MESH_PSK required when CLUSTER_ENABLED")
	}
	return cluster.NewTCPMesh(cluster.TCPMeshConfig{
		NodeID:         config.ClusterNodeID,
		ListenAddr:     config.ClusterListen,
		Peers:          peers,
		ClaimTimeout:   config.ClusterClaimTimeout,
		PeerDeathGrace: config.ClusterPeerDeathGrace,
		PSK:            config.ClusterPSK,
		Logger:         slog.Default(),
	})
}

func generateDefaultAdminUser(ctx context.Context, dbRepo *db.Repositories) (user *db.User, err error) {
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

	if err = dbRepo.UserRepo.CreateUser(ctx, user); err != nil {
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

	// Graceful mesh + interest publisher shutdown (issue 20).
	s.shutdownCluster()

	// Join service HTTP so callers (and tests) can close shared resources safely.
	select {
	case <-s.httpDone:
	case <-time.After(5 * time.Second):
	}

	return err
}

// shutdownCluster stops mesh, interest publisher, and auth/HomeRPC workers (R2-14).
func (s *Server) shutdownCluster() {
	if hr, ok := s.registry.(*HybridRegistry); ok {
		hr.StopInterest()
	}
	if s.mesh != nil {
		_ = s.mesh.Stop()
	}
	if s.authPool != nil {
		close(s.authPool)
		s.authPoolWG.Wait()
		s.authPool = nil
	}
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
