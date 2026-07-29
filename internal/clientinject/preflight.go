package clientinject

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrClientRunning indicates the primary PE appears locked / in use.
var ErrClientRunning = errors.New("clientinject: client appears to be running (file locked); quit the client completely, then Apply again")

// PreflightPrimaryPE tries to open the primary PE exclusively (O_RDWR +
// platform exclusive lock). On lock/sharing failures it returns
// ErrClientRunning. Best-effort: process-list detection is optional and not
// required for the gate.
func PreflightPrimaryPE(path string) error {
	if path == "" {
		return fmt.Errorf("clientinject: preflight: empty primary PE path")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if isLockError(err) {
			return fmt.Errorf("%w: open %s: %v", ErrClientRunning, path, err)
		}
		return fmt.Errorf("clientinject: preflight open %s: %w", path, err)
	}
	defer f.Close()

	if err := tryExclusiveLock(f); err != nil {
		if isLockError(err) {
			return fmt.Errorf("%w: lock %s: %v", ErrClientRunning, path, err)
		}
		return fmt.Errorf("clientinject: preflight lock %s: %w", path, err)
	}
	_ = unlockFile(f)
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
