package clientinject

import (
	"errors"
	"fmt"
	"strings"
)

// ErrClientRunning indicates the primary PE appears locked / in use.
var ErrClientRunning = errors.New("clientinject: client appears to be running (file locked); quit the client completely, then Apply again")

// PreflightPrimaryPE tries to open the primary PE exclusively.
// On Windows this is CreateFileW with dwShareMode=0 (no prior open handle).
// On Unix this is O_RDWR + flock(LOCK_EX|LOCK_NB).
// On lock/sharing failures it returns ErrClientRunning.
//
// This is a best-effort probe only: the exclusive lock is released before
// return. Engine.Apply does not hold the lock across CreateBackups/adapter
// mutations (TOCTOU is possible if the client starts mid-Apply). Process-list
// detection is optional and not required for the gate.
func PreflightPrimaryPE(path string) error {
	if path == "" {
		return fmt.Errorf("clientinject: preflight: empty primary PE path")
	}
	if err := platformPreflightExclusive(path); err != nil {
		if isLockError(err) {
			return fmt.Errorf("%w: lock %s: %v", ErrClientRunning, path, err)
		}
		return fmt.Errorf("clientinject: preflight %s: %w", path, err)
	}
	return nil
}

// isLockError reports platform messages that mean "file in use".
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrClientRunning) {
		return true
	}
	msg := strings.ToLower(err.Error())
	// Windows sharing / lock phrasing and Unix flock busy.
	for _, needle := range []string{
		"being used by another process",
		"error_sharing_violation",
		"sharing violation",
		"text file busy",
		"resource temporarily unavailable",
		"locked",
		"lock fail",
		"another program",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
