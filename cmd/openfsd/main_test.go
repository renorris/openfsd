package main

import (
	"os"
	"testing"

	"github.com/renorris/openfsd/internal/db"
)

func TestNormalizeColocatedMemoryDSN(t *testing.T) {
	t.Setenv("DATABASE_SOURCE_NAME", ":memory:")
	normalizeColocatedMemoryDSN(true, true, false)
	if got := os.Getenv("DATABASE_SOURCE_NAME"); got != db.SharedMemorySQLiteDSN {
		t.Fatalf("colocated fsd+web :memory: rewrite: got %q want %q", got, db.SharedMemorySQLiteDSN)
	}

	t.Setenv("DATABASE_SOURCE_NAME", ":memory:")
	normalizeColocatedMemoryDSN(true, false, true)
	if got := os.Getenv("DATABASE_SOURCE_NAME"); got != db.SharedMemorySQLiteDSN {
		t.Fatalf("colocated fsd+afv :memory: rewrite: got %q want %q", got, db.SharedMemorySQLiteDSN)
	}

	t.Setenv("DATABASE_SOURCE_NAME", ":memory:")
	normalizeColocatedMemoryDSN(false, true, true)
	if got := os.Getenv("DATABASE_SOURCE_NAME"); got != db.SharedMemorySQLiteDSN {
		t.Fatalf("colocated web+afv :memory: rewrite: got %q want %q", got, db.SharedMemorySQLiteDSN)
	}

	t.Setenv("DATABASE_SOURCE_NAME", ":memory:")
	normalizeColocatedMemoryDSN(true, false, false)
	if got := os.Getenv("DATABASE_SOURCE_NAME"); got != ":memory:" {
		t.Fatalf("fsd-only should leave :memory: alone, got %q", got)
	}

	t.Setenv("DATABASE_SOURCE_NAME", ":memory:")
	normalizeColocatedMemoryDSN(false, false, true)
	if got := os.Getenv("DATABASE_SOURCE_NAME"); got != ":memory:" {
		t.Fatalf("afv-only should leave :memory: alone, got %q", got)
	}

	t.Setenv("DATABASE_SOURCE_NAME", "openfsd.db")
	normalizeColocatedMemoryDSN(true, true, true)
	if got := os.Getenv("DATABASE_SOURCE_NAME"); got != "openfsd.db" {
		t.Fatalf("file DSN should be unchanged, got %q", got)
	}
}
