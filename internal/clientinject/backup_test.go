package clientinject

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveBackups(t *testing.T) {
	w := OSFileWriter{}
	dir := t.TempDir()
	orig := filepath.Join(dir, "a.bin")
	bak := BackupPath(orig)
	if err := w.WriteFile(orig, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := w.CopyFile(orig, bak); err != nil {
		t.Fatal(err)
	}
	if err := RemoveBackups(w, []ManifestFile{{Original: orig, Backup: bak}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bak); !os.IsNotExist(err) {
		t.Fatalf("bak should be gone: %v", err)
	}
}

func TestRestoreBackups_MissingBak(t *testing.T) {
	w := OSFileWriter{}
	err := RestoreBackups(w, []ManifestFile{{
		Original: filepath.Join(t.TempDir(), "o"),
		Backup:   filepath.Join(t.TempDir(), "missing.bak"),
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteManifest_Nil(t *testing.T) {
	if err := WriteManifest(OSFileWriter{}, nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadManifest_BadJSON(t *testing.T) {
	dir := t.TempDir()
	_ = OSFileWriter{}.WriteFile(ManifestPath(dir), []byte("{not json"))
	_, err := ReadManifest(OSFileWriter{}, dir)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCollectPlanTargets_NilAndPointerDetail(t *testing.T) {
	if CollectPlanTargets(nil) != nil {
		t.Fatal("nil plan")
	}
	plan := &Plan{
		Install: Install{RootDir: "/r", PrimaryPE: "pe.exe"},
		Mutations: []Mutation{{
			Detail: &ConfigRewriteDetail{Paths: []string{"cfg.xml"}},
		}},
	}
	got := CollectPlanTargets(plan)
	if len(got) < 2 {
		t.Fatalf("%v", got)
	}
}

func TestCreateBackups_SkipsMissing(t *testing.T) {
	w := OSFileWriter{}
	dir := t.TempDir()
	exist := filepath.Join(dir, "yes")
	_ = w.WriteFile(exist, []byte("1"))
	files, err := CreateBackups(w, []string{exist, filepath.Join(dir, "nope")})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("%v", files)
	}
}

// memWriter is a minimal FileWriter for error injection.
type memWriter struct {
	files     map[string][]byte
	copyErr   error
	writeErr  error
	statErr   map[string]error
	removeErr error
}

func newMem() *memWriter {
	return &memWriter{files: map[string][]byte{}, statErr: map[string]error{}}
}

func (m *memWriter) ReadFile(path string) ([]byte, error) {
	b, ok := m.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}
func (m *memWriter) WriteFile(path string, data []byte) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.files[path] = append([]byte(nil), data...)
	return nil
}
func (m *memWriter) CopyFile(src, dst string) error {
	if m.copyErr != nil {
		return m.copyErr
	}
	b, err := m.ReadFile(src)
	if err != nil {
		return err
	}
	return m.WriteFile(dst, b)
}
func (m *memWriter) OpenReadWrite(path string) (ReadWriteSeekCloser, error) {
	return nil, errors.New("not implemented")
}
func (m *memWriter) Stat(path string) (fs.FileInfo, error) {
	if e, ok := m.statErr[path]; ok {
		return nil, e
	}
	if _, ok := m.files[path]; !ok {
		return nil, os.ErrNotExist
	}
	return fakeInfo{name: filepath.Base(path), size: int64(len(m.files[path]))}, nil
}
func (m *memWriter) Remove(path string) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	delete(m.files, path)
	return nil
}
func (m *memWriter) MkdirAll(string, fs.FileMode) error { return nil }

type fakeInfo struct {
	name string
	size int64
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() fs.FileMode  { return 0o644 }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return nil }

func TestCreateBackups_CopyError(t *testing.T) {
	m := newMem()
	m.files["/a"] = []byte("x")
	m.copyErr = errors.New("copy fail")
	_, err := CreateBackups(m, []string{"/a"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRemoveBackups_Error(t *testing.T) {
	m := newMem()
	m.files["/a.bak"] = []byte("x")
	m.removeErr = errors.New("rm fail")
	err := RemoveBackups(m, []ManifestFile{{Backup: "/a.bak"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRestoreBackups_CopyError(t *testing.T) {
	m := newMem()
	m.files["/a.bak"] = []byte("x")
	m.copyErr = errors.New("copy fail")
	err := RestoreBackups(m, []ManifestFile{{Original: "/a", Backup: "/a.bak"}})
	if err == nil {
		t.Fatal("expected error")
	}
}
