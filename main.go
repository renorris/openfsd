package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/fsd"
	_ "modernc.org/sqlite"
	"os"
	"os/signal"
)

func main() {
	fmt.Println("hello world")

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

	user := &db.User{
		Password:      "12345",
		NetworkRating: 1,
	}
	err = dbRepo.UserRepo.CreateUser(user)
	if err != nil {
		panic(err)
	}
	fmt.Println(user)

	s, err := fsd.NewServer([]string{":6809"}, []byte("abcdef"), dbRepo)
	if err != nil {
		panic(err)
	}

	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)

	if err = s.Run(ctx); err != nil {
		fmt.Println(err)
	}
	fmt.Println("server closed")
}
