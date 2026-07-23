package db

import "testing"

func TestDSNConstants(t *testing.T) {
	if DefaultSQLiteDSN == "" || DefaultSQLiteDSN == ":memory:" {
		t.Fatalf("DefaultSQLiteDSN should be an on-disk path, got %q", DefaultSQLiteDSN)
	}
	if SharedMemorySQLiteDSN == "" || SharedMemorySQLiteDSN == ":memory:" {
		t.Fatalf("SharedMemorySQLiteDSN must not be bare :memory:, got %q", SharedMemorySQLiteDSN)
	}
}
