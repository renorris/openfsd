package web

import (
	"github.com/renorris/openfsd/database"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

func TestTemplateServer(tst *testing.T) {
	if !slices.Contains(os.Environ(), "TEMPLATE_SERVER=true") {
		return
	}

	mux := http.ServeMux{}

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		if _, err := w.Write(favicon); err != nil {
			log.Println(err)
		}
	})

	mux.Handle("/static/", http.FileServerFS(StaticFS))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		base := filepath.Base(r.URL.Path) + ".html"

		pageData := DashboardPageData{
			UserRecord: &database.FSDUserRecord{
				CID:           123456,
				Email:         "example@mail.com",
				FirstName:     "Foo",
				LastName:      "Bar",
				Password:      "",
				FSDPassword:   "plaitextfsdpassword",
				NetworkRating: 12,
				CreatedAt:     time.Time{},
				UpdatedAt:     time.Time{},
			},
		}

		if err := RenderTemplate(w, base, pageData); err != nil {
			w.WriteHeader(404)
			log.Println(err)
			return
		}
	})

	server := http.Server{
		Addr:    "localhost:8080",
		Handler: &mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
}
