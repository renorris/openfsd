//go:build unix

package clientinject

import (
	"fmt"
	"os"
	"syscall"
)

// platformPreflightExclusive opens the path O_RDWR and takes a non-blocking
// exclusive flock, then releases both.
func platformPreflightExclusive(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if isLockError(err) {
			return fmt.Errorf("%w: open %s: %v", ErrClientRunning, path, err)
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	if err := tryExclusiveLock(f); err != nil {
		return err
	}
	_ = unlockFile(f)
	return nil
}

func tryExclusiveLock(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// EWOULDBLOCK / EAGAIN → in use
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return fmt.Errorf("%w: flock: %v", ErrClientRunning, err)
		}
		return err
	}
	return nil
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
