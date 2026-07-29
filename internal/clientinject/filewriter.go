package clientinject

import (
	"fmt"
	"io"
	"io/fs"
	"os"
)

// ReadWriteSeekCloser is the handle returned by FileWriter.OpenReadWrite.
type ReadWriteSeekCloser interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	Truncate(size int64) error
}

// FileWriter abstracts disk for tests and transactional Apply.
type FileWriter interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte) error
	CopyFile(src, dst string) error
	OpenReadWrite(path string) (ReadWriteSeekCloser, error)
	Stat(path string) (fs.FileInfo, error)
	Remove(path string) error
	MkdirAll(path string, perm fs.FileMode) error
}

// OSFileWriter implements FileWriter with the os package.
type OSFileWriter struct{}

// ReadFile implements FileWriter.
func (OSFileWriter) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile implements FileWriter.
// When overwriting an existing file, preserves its permission bits (so PE
// rewrites via full-file WriteFile keep the executable bit on Unix/Wine).
// New files default to 0o644. Prefer OpenReadWrite / pepatch for PE mutations.
func (OSFileWriter) WriteFile(path string, data []byte) error {
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
	}
	return os.WriteFile(path, data, mode)
}

// CopyFile implements FileWriter.
func (OSFileWriter) CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("clientinject: open src %s: %w", src, err)
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("clientinject: stat src %s: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("clientinject: open dst %s: %w", dst, err)
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("clientinject: copy %s -> %s: %w", src, dst, copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("clientinject: sync %s: %w", dst, syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("clientinject: close %s: %w", dst, closeErr)
	}
	return nil
}

// OpenReadWrite implements FileWriter.
func (OSFileWriter) OpenReadWrite(path string) (ReadWriteSeekCloser, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Stat implements FileWriter.
func (OSFileWriter) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

// Remove implements FileWriter.
func (OSFileWriter) Remove(path string) error {
	return os.Remove(path)
}

// MkdirAll implements FileWriter.
func (OSFileWriter) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Ensure OSFileWriter satisfies FileWriter.
var _ FileWriter = OSFileWriter{}
