package fsdclient

import (
	"errors"
	"fmt"
)

// Common sentinel errors.
var (
	// ErrClosed is returned when operating on a closed client.
	ErrClosed = errors.New("fsdclient: client closed")
	// ErrNotDialed is returned when methods are used before Dial.
	ErrNotDialed = errors.New("fsdclient: not dialed")
	// ErrBadServerIdent is returned when the first server packet is not a valid $DI.
	ErrBadServerIdent = errors.New("fsdclient: expected $DISERVER:CLIENT server identification")
	// ErrAlreadyLoggedIn is returned when LoginPilot/LoginATC is called twice.
	ErrAlreadyLoggedIn = errors.New("fsdclient: already logged in")
)

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
