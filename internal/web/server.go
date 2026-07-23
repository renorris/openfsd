package web

import (
	"context"
	"database/sql"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"net"
	"text/template"

	"github.com/renorris/openfsd/internal/db"
)

type Server struct {
	cfg                *ServerConfig
	dbRepo             *db.Repositories
	statusTxtTemplate  *template.Template
	serversTxtTemplate *template.Template
	pageTemplates      map[string]*htmltemplate.Template
}

func NewDefaultServer(ctx context.Context) (server *Server, err error) {
	cfg, err := loadServerConfig(ctx)
	if err != nil {
		return
	}

	if err = db.RequireSQLiteDriver(cfg.DatabaseDriver); err != nil {
		return
	}

	slog.Info("using sqlite")

	slog.Debug("connecting to SQL")
	sqlDb, err := sql.Open("sqlite", cfg.DatabaseSourceName)
	if err != nil {
		return
	}
	slog.Debug("SQL OK")

	if err = sqlDb.PingContext(ctx); err != nil {
		return
	}

	sqlDb.SetMaxOpenConns(cfg.DatabaseMaxConns)

	// Migrate here too so -web alone (or web starting against a fresh file)
	// works. When colocated, FSD migrates first; second Up is ErrNoChange.
	if cfg.DatabaseAutoMigrate {
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

	// Seed JWT/welcome defaults if missing (SetIfNotExists). Safe when FSD
	// already initialized config on the same database.
	if err = db.InitDefaultConfig(dbRepo.ConfigRepo); err != nil {
		return
	}

	if server, err = NewServer(cfg, dbRepo); err != nil {
		return
	}

	return
}

func NewServer(cfg *ServerConfig, dbRepo *db.Repositories) (server *Server, err error) {
	statusTxt, serversTxt, err := parseDataTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse data templates: %w", err)
	}

	pageTemplates, err := parsePageTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse page templates: %w", err)
	}

	server = &Server{
		cfg:                cfg,
		dbRepo:             dbRepo,
		statusTxtTemplate:  statusTxt,
		serversTxtTemplate: serversTxt,
		pageTemplates:      pageTemplates,
	}

	return
}

func (s *Server) Run(ctx context.Context) (err error) {
	e, err := s.setupRoutes()
	if err != nil {
		return err
	}
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
