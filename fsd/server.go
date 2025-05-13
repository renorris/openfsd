package fsd

import (
	"context"
	"errors"
	"fmt"
	"github.com/renorris/openfsd/db"
	"net"
	"sync"
)

type Server struct {
	listenAddrs  []string
	jwtSecret    []byte
	postOffice   *postOffice
	metarService *metarService
	dbRepo       *db.Repositories
}

func NewServer(listenAddrs []string, jwtSecret []byte, dbRepo *db.Repositories) (server *Server, err error) {
	server = &Server{
		listenAddrs:  listenAddrs,
		jwtSecret:    jwtSecret,
		postOffice:   newPostOffice(),
		metarService: newMetarService(4),
		dbRepo:       dbRepo,
	}
	return
}

func (s *Server) Run(ctx context.Context) (err error) {
	// Start metar service
	go s.metarService.run(ctx)

	errCh := make(chan error, len(s.listenAddrs))
	var listenerWg sync.WaitGroup

	for _, addr := range s.listenAddrs {
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

	return ctx.Err()
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
