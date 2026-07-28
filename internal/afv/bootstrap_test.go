package afv_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/renorris/openfsd/internal/afv"
)

func TestNewDefault(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "openfsd.db")
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", dbPath)
	t.Setenv("DATABASE_AUTO_MIGRATE", "true")
	t.Setenv("AFV_UDP_ADVERTISE_IPV4", "127.0.0.1:50000")
	t.Setenv("AFV_API_LISTEN", "127.0.0.1:0")
	t.Setenv("AFV_UDP_LISTEN", "127.0.0.1:0")
	t.Setenv("AFV_JWT_SECRET", "bootstrap-test-secret-key-material")

	srv, err := afv.NewDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if srv == nil || srv.Registry() == nil {
		t.Fatal("nil server")
	}
}

func TestNewDefaultRequiresAdvertise(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", filepath.Join(dir, "x.db"))
	t.Setenv("DATABASE_AUTO_MIGRATE", "true")
	t.Setenv("AFV_UDP_ADVERTISE_IPV4", "")
	t.Setenv("AFV_JWT_SECRET", "x")
	if _, err := afv.NewDefault(context.Background()); err == nil {
		t.Fatal("expected error without advertise")
	}
}
