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

	if err = db.RequireDatabaseDriver(cfg.DatabaseDriver); err != nil {
		return
	}

	driver := cfg.DatabaseDriver
	if driver == "" {
		driver = "sqlite"
	}
	slog.Info("using database driver", "driver", driver)

	readLevel, errRL := db.ParseReadLevel(cfg.AuthReadLevel)
	if errRL != nil {
		err = errRL
		return
	}

	// Migrate sqlite locally; rqlite only if migrate leader.
	if cfg.DatabaseAutoMigrate && (driver == "sqlite" || driver == "") {
		slog.Debug("automatically migrating database")
		sqlDb, errOpen := sql.Open("sqlite", cfg.DatabaseSourceName)
		if errOpen != nil {
			err = errOpen
			return
		}
		if err = sqlDb.PingContext(ctx); err != nil {
			_ = sqlDb.Close()
			return
		}
		sqlDb.SetMaxOpenConns(cfg.DatabaseMaxConns)
		if err = db.Migrate(sqlDb); err != nil {
			_ = sqlDb.Close()
			return
		}
		_ = sqlDb.Close()
		slog.Debug("migrate OK")
	} else if cfg.DatabaseAutoMigrate && driver == "rqlite" && cfg.DatabaseMigrateLeader {
		client := db.NewRqliteClient(cfg.DatabaseSourceName, db.RqliteClientOptions{})
		if err = db.MigrateRqlite(ctx, client); err != nil {
			return
		}
	}

	dbRepo, err := db.OpenRepositories(ctx, driver, cfg.DatabaseSourceName, readLevel, true)
	if err != nil {
		return
	}

	// Seed JWT/welcome defaults if missing (SetIfNotExists). Safe when FSD
	// already initialized config on the same database.
	if err = db.InitDefaultConfig(ctx, dbRepo.ConfigRepo); err != nil {
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
