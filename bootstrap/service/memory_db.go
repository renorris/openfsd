package service

import (
	"context"
	sqle "github.com/dolthub/go-mysql-server"
	"github.com/dolthub/go-mysql-server/memory"
	"github.com/dolthub/go-mysql-server/server"
	"github.com/fatih/color"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/servercontext"
	"log"
	"net"
)

type InMemoryDatabaseService struct{}

func (s *InMemoryDatabaseService) Start(ctx context.Context, doneErr chan<- error) error {
	ready := make(chan struct{})

	go func(ctx context.Context, doneErr chan<- error, ready chan struct{}) {
		doneErr <- s.boot(ctx, ready)
	}(ctx, doneErr, ready)

	// wait for ready signal
	<-ready

	// Initialize database
	return database.Initialize(servercontext.DB())
}

func (s *InMemoryDatabaseService) boot(ctx context.Context, ready chan struct{}) (err error) {
	db := memory.NewDatabase("openfsd")
	db.BaseDatabase.EnablePrimaryKeyIndexes()

	pro := memory.NewDBProvider(db)
	engine := sqle.NewDefault(pro)

	var listener net.Listener
	if listener, err = net.Listen("tcp", "127.0.0.1:33060"); err != nil {
		close(ready)
		return err
	}

	close(ready)

	config := server.Config{
		Protocol: "tcp",
		Listener: listener,
	}

	log.Println(color.YellowString("WARNING: ") + "using an ephemeral in-memory database. This should only be used for testing. All data will be lost when the process ends.")
	log.Printf("In-memory MySQL server listening on 127.0.0.1:33060")
	defer log.Println("In-memory MySQL server shutting down...")

	var srv *server.Server
	if srv, err = server.NewServer(config, engine, memory.NewSessionBuilder(pro), nil); err != nil {
		return err
	}

	errCh := make(chan error)
	go func() { errCh <- srv.Start() }()

	select {
	case <-ctx.Done():
		return srv.Close()
	case err = <-errCh:
		return err
	}
}
