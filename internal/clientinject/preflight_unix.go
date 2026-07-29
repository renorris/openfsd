//go:build unix

package clientinject

import (
	"fmt"
	"os"
	"syscall"
)

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
