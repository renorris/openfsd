package fsd

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/renorris/openfsd/db"
	"io"
	"log/slog"
	"net"
	"sync"
)

type Server struct {
	cfg          *ServerConfig
	postOffice   *postOffice
	metarService *metarService
	dbRepo       *db.Repositories
}

// NewServer creates a new Server instance.
//
// See NewDefaultServer to create a server using default settings obtained via environment variables.
func NewServer(cfg *ServerConfig, dbRepo *db.Repositories, numMetarWorkers int) (server *Server, err error) {
	server = &Server{
		cfg:          cfg,
		postOffice:   newPostOffice(),
		metarService: newMetarService(numMetarWorkers),
		dbRepo:       dbRepo,
	}
	return
}

// NewDefaultServer creates a new Server instance using the default configuration obtained via environment variables
func NewDefaultServer(ctx context.Context) (server *Server, err error) {
	config, err := loadServerConfig(ctx)
	if err != nil {
		return
	}

	slog.Info(fmt.Sprintf("using %s", config.DatabaseDriver))

	slog.Debug("connecting to SQL")
	sqlDb, err := sql.Open(config.DatabaseDriver, config.DatabaseSourceName)
	if err != nil {
		return
	}
	slog.Debug("SQL opened")

	if err = sqlDb.PingContext(ctx); err != nil {
		return
	}

	sqlDb.SetMaxOpenConns(config.DatabaseMaxConns)

	if config.DatabaseAutoMigrate {
		slog.Debug("automatically migrating database")
		if err = db.Migrate(sqlDb); err != nil {
			return
		}
		slog.Debug("migrate OK")
	}

	dbRepo, err := db.NewRepositories(sqlDb)
	if err != nil {
		return
	}

	// Generate a default admin user if CID 1 isn't taken
	if _, err = dbRepo.UserRepo.GetUserByCID(1); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return
		}
		err = nil

		slog.Debug("no user with CID = 1 found, creating default admin user")
		var user *db.User
		if user, err = generateDefaultAdminUser(dbRepo); err != nil {
			return
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
	if err = db.InitDefaultConfig(&dbRepo.ConfigRepo); err != nil {
		return
	}
	slog.Debug("config OK")

	if server, err = NewServer(config, dbRepo, config.NumMetarWorkers); err != nil {
		return
	}

	return
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
		NetworkRating: int(NetworkRatingAdministator),
	}

	if err = dbRepo.UserRepo.CreateUser(user); err != nil {
		return
	}

	return
}

func (s *Server) Run(ctx context.Context) (err error) {
	// Start metar service
	go s.metarService.run(ctx)

	// Start HTTP service
	go s.runServiceHTTP(ctx)

	errCh := make(chan error, len(s.cfg.FsdListenAddrs))
	var listenerWg sync.WaitGroup

	for _, addr := range s.cfg.FsdListenAddrs {
		slog.Info(fmt.Sprintf("Listening on %s\n", addr))
		listenerWg.Add(1)
		go func(ctx context.Context, addr string) {
			defer listenerWg.Done()
			s.listen(ctx, addr, errCh)
		}(ctx, addr)
	}

	// Collect startup errors
	go func() {
		listenerWg.Wait()
		close(errCh)
	}()

	var startupErrors []error
	for err := range errCh {
		startupErrors = append(startupErrors, err)
	}

	if len(startupErrors) > 0 {
		return fmt.Errorf("some listeners failed: %v", startupErrors)
	}

	// All listeners started successfully; wait for context to be cancelled
	<-ctx.Done()

	return
}

func (s *Server) listen(ctx context.Context, addr string, errCh chan<- error) {
	config := net.ListenConfig{}
	listener, err := config.Listen(ctx, "tcp4", addr)
	if err != nil {
		errCh <- fmt.Errorf("failed to listen on %s: %w", addr, err)
		return
	}
	defer listener.Close()

	// Start a goroutine to close the listener when the context is cancelled
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	// Accept connections in a loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				// Listener was closed due to context cancellation; exit the loop
				return
			}
			// Log or handle non-fatal accept errors
			continue
		}
		// Handle the connection in another goroutine
		go s.handleConn(ctx, conn)
	}
}
