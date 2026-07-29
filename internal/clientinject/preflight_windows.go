//go:build windows

package clientinject

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// On Windows, CreateFileW with dwShareMode=0 is the real "client running" gate:
// a mapped/open PE typically fails exclusive open with ERROR_SHARING_VIOLATION
// even when no byte-range locks exist. LockFileEx alone is insufficient because
// running EXEs usually do not hold range locks — LockFileEx can succeed while
// the client is still running.
//
// Important: do NOT open the path with os.OpenFile before CreateFile exclusive.
// A same-process open handle makes CreateFile(share=0) always fail with
// ERROR_SHARING_VIOLATION (confirmed on Windows 10/11). Path-based exclusive
// open is the only correct preflight pattern on Windows.

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileW  = modkernel32.NewProc("CreateFileW")
	procCloseHandle  = modkernel32.NewProc("CloseHandle")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	_GENERIC_READ              = 0x80000000
	_GENERIC_WRITE             = 0x40000000
	_OPEN_EXISTING             = 3
	_FILE_ATTRIBUTE_NORMAL     = 0x80
	_INVALID_HANDLE_VALUE      = ^uintptr(0)
	_LOCKFILE_EXCLUSIVE_LOCK   = 0x00000002
	_LOCKFILE_FAIL_IMMEDIATELY = 0x00000001
	_ERROR_LOCK_VIOLATION      = 33
	_ERROR_SHARING_VIOLATION   = 32
)

// platformPreflightExclusive is the Windows "client running" probe: CreateFile
// with share mode 0, then close. No long-lived handle.
func platformPreflightExclusive(path string) error {
	return tryCreateFileExclusive(path)
}

// tryExclusiveLock is used by tests that already hold an *os.File.
// Prefer platformPreflightExclusive for production preflight.
// CreateFile exclusive is attempted only after the caller's handle would
// conflict — so this path uses LockFileEx on the existing handle only.
func tryExclusiveLock(f *os.File) error {
	var ol syscall.Overlapped
	r1, _, e1 := procLockFileEx.Call(
		f.Fd(),
		uintptr(_LOCKFILE_EXCLUSIVE_LOCK|_LOCKFILE_FAIL_IMMEDIATELY),
		0,
		1, // lock 1 byte
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		errno := e1
		if errno == syscall.Errno(_ERROR_LOCK_VIOLATION) ||
			errno == syscall.Errno(_ERROR_SHARING_VIOLATION) {
			return fmt.Errorf("%w: LockFileEx: %v", ErrClientRunning, errno)
		}
		// Non-lock LockFileEx failure: treat as probe noise.
		return nil
	}
	return nil
}

func unlockFile(f *os.File) error {
	var ol syscall.Overlapped
	r1, _, e1 := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r1 == 0 {
		return e1
	}
	return nil
}

func tryCreateFileExclusive(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r1, _, e1 := procCreateFileW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(_GENERIC_READ|_GENERIC_WRITE),
		0, // share mode 0 = exclusive
		0,
		uintptr(_OPEN_EXISTING),
		uintptr(_FILE_ATTRIBUTE_NORMAL),
		0,
	)
	if r1 == _INVALID_HANDLE_VALUE || r1 == 0 {
		if e1 == syscall.Errno(_ERROR_SHARING_VIOLATION) ||
			e1 == syscall.Errno(_ERROR_LOCK_VIOLATION) {
			return fmt.Errorf("%w: CreateFile: %v", ErrClientRunning, e1)
		}
		return e1
	}
	procCloseHandle.Call(r1)
	return nil
}
