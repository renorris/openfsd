package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/internal/web"
)

func main() {
	setSlogLevel()

	fsdFlag := flag.Bool("fsd", false, "run the FSD server (TCP protocol + internal service HTTP)")
	webFlag := flag.Bool("web", false, "run the web UI and /api/v1 HTTP server")
	flag.Parse()

	runFSD, runWeb := *fsdFlag, *webFlag
	// Default: both services when neither flag is set.
	if !runFSD && !runWeb {
		runFSD, runWeb = true, true
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, runFSD, runWeb); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run(parent context.Context, runFSD, runWeb bool) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	errCh := make(chan error, 2)
	var wg sync.WaitGroup

	if runFSD {
		// Construct first so migrations/admin seed complete before the web
		// process opens the same database (colocated mode).
		fsdSrv, err := server.NewDefault(ctx)
		if err != nil {
			return fmt.Errorf("fsd: %w", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fsdSrv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- fmt.Errorf("fsd: %w", err)
				cancel()
				return
			}
			slog.Info("FSD server closed")
			errCh <- nil
		}()

		if runWeb {
			if err := waitForFSDServiceHTTP(ctx); err != nil {
				cancel()
				wg.Wait()
				return err
			}
		}
	}

	if runWeb {
		// Colocated default: web talks to the in-process FSD service HTTP.
		if os.Getenv("FSD_HTTP_SERVICE_ADDRESS") == "" {
			_ = os.Setenv("FSD_HTTP_SERVICE_ADDRESS", "http://127.0.0.1:13618")
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := web.Main(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errCh <- fmt.Errorf("web: %w", err)
				cancel()
				return
			}
			slog.Info("web server closed")
			errCh <- nil
		}()
	}

	expected := 0
	if runFSD {
		expected++
	}
	if runWeb {
		expected++
	}

	var firstErr error
	for i := 0; i < expected; i++ {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	wg.Wait()
	return firstErr
}

// waitForFSDServiceHTTP polls until the FSD internal service port accepts TCP.
// Used when both services run in one process so the web layer does not race
// the service HTTP bind.
func waitForFSDServiceHTTP(ctx context.Context) error {
	addr := os.Getenv("SERVICE_HTTP_LISTEN_ADDR")
	if addr == "" {
		addr = ":13618"
	}
	// net.Dial wants host:port; bare ":port" is fine for local listen checks.
	dialAddr := addr
	if strings.HasPrefix(dialAddr, ":") {
		dialAddr = "127.0.0.1" + dialAddr
	}

	deadline := time.Now().Add(15 * time.Second)
	var d net.Dialer
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for FSD service HTTP: %w", err)
		}
		c, err := d.DialContext(ctx, "tcp", dialAddr)
		if err == nil {
			_ = c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for FSD service HTTP on %s: %w", dialAddr, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for FSD service HTTP: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func setSlogLevel() {
	if os.Getenv("LOG_DEBUG") == "true" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
}
