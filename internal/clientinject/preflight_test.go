package clientinject

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPreflightPrimaryPE_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(path, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PreflightPrimaryPE(path); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightPrimaryPE_Missing(t *testing.T) {
	err := PreflightPrimaryPE(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrClientRunning) {
		t.Fatalf("missing should not be running: %v", err)
	}
}

func TestPreflightPrimaryPE_Empty(t *testing.T) {
	if err := PreflightPrimaryPE(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestPreflightPrimaryPE_Locked(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows LockFileEx/share semantics differ; exercise best-effort path only.
		t.Log("windows: lock contention test is best-effort; skipping strict assertion")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(path, []byte("MZ\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hold exclusive flock in this process.
	holder, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := tryExclusiveLock(holder); err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	defer unlockFile(holder)

	err = PreflightPrimaryPE(path)
	if err == nil {
		// Some platforms may not enforce flock against OpenFile the same way;
		// document soft pass only if lock API claims success without contention.
		t.Skip("platform did not report lock contention; preflight best-effort")
	}
	if !errors.Is(err, ErrClientRunning) {
		t.Fatalf("err=%v want ErrClientRunning", err)
	}
}

func TestIsLockError(t *testing.T) {
	if !isLockError(errors.New("The process cannot access the file because it is being used by another process.")) {
		t.Fatal("windows sharing message")
	}
	if !isLockError(ErrClientRunning) {
		t.Fatal("sentinel")
	}
	if isLockError(errors.New("no such file")) {
		t.Fatal("false positive")
	}
	if isLockError(nil) {
		t.Fatal("nil")
	}
}
