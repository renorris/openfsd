package main

import (
	"context"
	"github.com/renorris/openfsd/db"
)

type Server struct {
	dbRepo    *db.Repositories
	jwtSecret []byte
}

func NewServer(dbRepo *db.Repositories, jwtSecret []byte) (server *Server, err error) {
	server = &Server{
		dbRepo:    dbRepo,
		jwtSecret: jwtSecret,
	}

	return
}

func (s *Server) Run(ctx context.Context, addr string) (err error) {
	e := s.setupRoutes()
	e.Run(addr)

	return
}
