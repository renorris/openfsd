package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/renorris/openfsd/db"
	"log/slog"
	"net"
)

type Server struct {
	cfg    *ServerConfig
	dbRepo *db.Repositories
}

func NewDefaultServer(ctx context.Context) (server *Server, err error) {
	cfg, err := loadServerConfig(ctx)
	if err != nil {
		return
	}

	slog.Info(fmt.Sprintf("using %s", cfg.DatabaseDriver))

	slog.Debug("connecting to SQL")
	sqlDb, err := sql.Open(cfg.DatabaseDriver, cfg.DatabaseSourceName)
	if err != nil {
		return
	}
	slog.Debug("SQL OK")

	sqlDb.SetMaxOpenConns(cfg.DatabaseMaxConns)

	dbRepo, err := db.NewRepositories(sqlDb)
	if err != nil {
		return
	}

	if server, err = NewServer(cfg, dbRepo); err != nil {
		return
	}

	return
}

func NewServer(cfg *ServerConfig, dbRepo *db.Repositories) (server *Server, err error) {
	server = &Server{
		cfg:    cfg,
		dbRepo: dbRepo,
	}

	return
}

func (s *Server) Run(ctx context.Context) (err error) {
	e := s.setupRoutes()
	go s.runDatafeedWorker(ctx)

	listener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return
	}
	defer listener.Close()

	go func() {
		if err := e.RunListener(listener); err != nil {
			slog.Error(err.Error())
		}
	}()

	<-ctx.Done()
	
	return
}
