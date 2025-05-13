package main

import (
	"context"
	"database/sql"
	"github.com/renorris/openfsd/db"
)

func main() {
	sqlDb, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}

	if err = db.Migrate(sqlDb); err != nil {
		panic(err)
	}

	dbRepo, err := db.NewRepositories(sqlDb)
	if err != nil {
		panic(err)
	}

	if err = dbRepo.UserRepo.CreateUser(&db.User{
		Password:      "12345",
		NetworkRating: 12,
	}); err != nil {
		panic(err)
	}

	server, err := NewServer(dbRepo, []byte("abcdef"))
	if err != nil {
		panic(err)
	}

	server.Run(context.Background(), "0.0.0.0:8080")
}
