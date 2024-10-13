package service

import (
	"context"
	"github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/servercontext"
	"github.com/renorris/openfsd/web"
	"log"
	"net"
	"net/http"
)

type HTTPService struct{}

func (s *HTTPService) Start(ctx context.Context, doneErr chan<- error) (err error) {

	// Attempt to listen. Once a listener is acquired, we can guarantee that clients can connect.
	var listener net.Listener
	if listener, err = net.Listen("tcp", servercontext.Config().HTTPListenAddress); err != nil {
		return err
	}

	log.Printf("HTTP server listening on %s", servercontext.Config().HTTPListenAddress)

	// boot http server on its own goroutine
	go func(ctx context.Context, doneErr chan<- error, listener net.Listener) {
		doneErr <- s.boot(ctx, listener)
	}(ctx, doneErr, listener)

	return nil
}

func (s *HTTPService) boot(ctx context.Context, listener net.Listener) (err error) {
	defer listener.Close()

	defer log.Println("HTTP server shutting down...")

	mux := http.NewServeMux()

	// token provider
	mux.HandleFunc("POST /api/v1/fsd-jwt", web.FSDJWTHandler)

	// api/v1 users
	mux.HandleFunc("GET /api/v1/users/{cid}", func(w http.ResponseWriter, r *http.Request) {
		web.APIV1UsersHandler(w, r, auth.DefaultVerifier{})
	})
	mux.HandleFunc("/api/v1/users", func(w http.ResponseWriter, r *http.Request) {
		web.APIV1UsersHandler(w, r, auth.DefaultVerifier{})
	})

	// data feed
	mux.HandleFunc("GET /api/v1/data/openfsd-data.json", web.DataFeedHandler)

	// status.txt, servers.txt, servers.json
	mux.HandleFunc("GET /api/v1/data/status.txt", web.StatusTxtHandler)
	mux.HandleFunc("GET /api/v1/data/servers.txt", web.ServerListTxtHandler)
	mux.HandleFunc("GET /api/v1/data/servers.json", web.ServerListJsonHandler)

	// favicon
	mux.HandleFunc("/favicon.ico", web.FaviconHandler)

	// static files
	mux.Handle("/static/", http.FileServerFS(web.StaticFS))

	// Interface UI handler (catch-all)
	mux.HandleFunc("/", web.FrontendHandler)

	httpServer := &http.Server{
		Addr:    servercontext.Config().HTTPListenAddress,
		Handler: mux,
	}

	errCh := make(chan error)
	go func() {
		if servercontext.Config().TLSCertFile != "" && servercontext.Config().TLSKeyFile != "" {
			errCh <- httpServer.ServeTLS(listener, servercontext.Config().TLSCertFile, servercontext.Config().TLSKeyFile)
		} else {
			errCh <- httpServer.Serve(listener)
		}
	}()

	select {
	case <-ctx.Done():
		return httpServer.Close()
	case err = <-errCh:
		return err
	}
}
