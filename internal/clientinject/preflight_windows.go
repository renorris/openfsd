//go:build windows

package clientinject

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// On Windows, a second CreateFile with share-mode 0 fails if the PE is mapped
// or open without share. We re-open via CreateFileW with dwShareMode=0.
// If that fails with sharing violation, treat as client running.
// Holding a simple *os.File from OpenFile may still allow another OpenFile
// (Go uses FILE_SHARE_READ|WRITE); exclusive CreateFile is the real gate.

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

func tryExclusiveLock(f *os.File) error {
	// Prefer LockFileEx exclusive non-blocking on the already-open handle.
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
		// Fallback: try CreateFile with share mode 0.
		return tryCreateFileExclusive(f.Name())
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
