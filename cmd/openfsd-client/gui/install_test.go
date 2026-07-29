package gui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/renorris/openfsd/internal/clientinject"
)

// installFakeAdapter is a minimal Adapter for BuildInstall / FirstDetectPath tests.
type installFakeAdapter struct {
	id    string
	name  string
	cands []clientinject.InstallCandidate
}

func (a *installFakeAdapter) ClientID() string            { return a.id }
func (a *installFakeAdapter) DisplayName() string         { return a.name }
func (a *installFakeAdapter) SupportedProfiles() []string { return []string{"fake-1"} }
func (a *installFakeAdapter) Discover(context.Context) ([]clientinject.InstallCandidate, error) {
	return append([]clientinject.InstallCandidate(nil), a.cands...), nil
}
func (a *installFakeAdapter) Verify(clientinject.Install, *clientinject.Profile) error {
	return nil
}
func (a *installFakeAdapter) EndpointConstraints(*clientinject.Profile) []clientinject.Constraint {
	return nil
}
func (a *installFakeAdapter) Plan(clientinject.Install, *clientinject.Profile, clientinject.Endpoints) (*clientinject.Plan, error) {
	return &clientinject.Plan{}, nil
}
func (a *installFakeAdapter) Apply(context.Context, *clientinject.Plan, clientinject.FileWriter) error {
	return nil
}
func (a *installFakeAdapter) HealthCheck(clientinject.Install, clientinject.Endpoints) error {
	return nil
}
func (a *installFakeAdapter) LaunchArgs(clientinject.Install, clientinject.Endpoints) []string {
	return nil
}

func TestBuildInstall_FromProfileRelativePath(t *testing.T) {
	root := t.TempDir()
	peRel := "app.exe"
	cfgRel := "config.xml"
	if err := os.WriteFile(filepath.Join(root, peRel), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, cfgRel), []byte("<x/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := clientinject.NewProfileStore()
	if err := store.Add(&clientinject.Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: clientinject.PrimaryBinarySpec{RelativePath: peRel},
		ConfigFiles:   []clientinject.ConfigFileSpec{{RelativePath: cfgRel}},
	}); err != nil {
		t.Fatal(err)
	}
	eng := clientinject.NewEngine(store) // no adapter — profile fallback path
	install := BuildInstall(eng, "fake", root)
	if install.PrimaryPE != filepath.Join(root, peRel) {
		t.Fatalf("PrimaryPE=%q", install.PrimaryPE)
	}
	if len(install.ConfigPaths) != 1 || install.ConfigPaths[0] != filepath.Join(root, cfgRel) {
		t.Fatalf("ConfigPaths=%v", install.ConfigPaths)
	}
	if install.ClientID != "fake" || install.RootDir != filepath.Clean(root) {
		t.Fatalf("%+v", install)
	}
}

func TestBuildInstall_DiscoverMatch(t *testing.T) {
	root := t.TempDir()
	pe := filepath.Join(root, "client.exe")
	cfg := filepath.Join(root, "cfg.xml")
	if err := os.WriteFile(pe, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &installFakeAdapter{
		id:   "fake",
		name: "Fake",
		cands: []clientinject.InstallCandidate{{
			ClientID:    "fake",
			RootDir:     root,
			PrimaryPE:   pe,
			ConfigPaths: []string{cfg},
			DisplayHint: "test install",
		}},
	}
	eng := clientinject.NewEngine(clientinject.NewProfileStore(), fake)
	install := BuildInstall(eng, "fake", root)
	if install.PrimaryPE != pe {
		t.Fatalf("PrimaryPE=%q want %q", install.PrimaryPE, pe)
	}
	if len(install.ConfigPaths) != 1 || install.ConfigPaths[0] != cfg {
		t.Fatalf("ConfigPaths=%v", install.ConfigPaths)
	}
}

func TestBuildInstall_ManifestSeed(t *testing.T) {
	root := t.TempDir()
	peRel := "app.exe"
	if err := os.WriteFile(filepath.Join(root, peRel), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := clientinject.NewProfileStore()
	if err := store.Add(&clientinject.Profile{
		SchemaVersion: 2,
		ProfileID:     "fake-1",
		ClientID:      "fake",
		PrimaryBinary: clientinject.PrimaryBinarySpec{RelativePath: peRel, SHA1: "abc"},
	}); err != nil {
		t.Fatal(err)
	}
	// Write a minimal inject manifest.
	m := clientinject.Manifest{
		ClientID:    "fake",
		ProfileID:   "fake-1",
		PESHA1:      "deadbeefcafebabe",
		Status:      clientinject.ManifestStatusApplied,
		InstallRoot: root,
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clientinject.ManifestPath(root), data, 0o644); err != nil {
		t.Fatal(err)
	}

	eng := clientinject.NewEngine(store)
	install := BuildInstall(eng, "fake", root)
	if install.ProfileID != "fake-1" {
		t.Fatalf("ProfileID=%q", install.ProfileID)
	}
	if install.HashSHA1 != "deadbeefcafebabe" {
		t.Fatalf("HashSHA1=%q", install.HashSHA1)
	}
}

func TestFirstDetectPath(t *testing.T) {
	root := t.TempDir()
	pe := filepath.Join(root, "x.exe")
	fake := &installFakeAdapter{
		id: "fake",
		cands: []clientinject.InstallCandidate{{
			ClientID:  "fake",
			RootDir:   root,
			PrimaryPE: pe,
		}},
	}
	eng := clientinject.NewEngine(nil, fake)
	cand, ok := FirstDetectPath(eng, "fake")
	if !ok || cand.RootDir != root {
		t.Fatalf("ok=%v cand=%+v", ok, cand)
	}
	if _, ok := FirstDetectPath(eng, "missing"); ok {
		t.Fatal("expected no candidate")
	}
	if _, ok := FirstDetectPath(nil, "fake"); ok {
		t.Fatal("nil engine")
	}
}

func TestSettingsFromForm_RoundTrip(t *testing.T) {
	f := FormState{
		ClientID:    "vpilot",
		InstallPath: "/x",
		WebBaseURL:  "https://fsd.ex.co",
		FSDHost:     "h",
	}
	s := SettingsFromForm(f)
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	var f2 FormState
	got.ApplyToForm(&f2)
	if f2.WebBaseURL != f.WebBaseURL || f2.InstallPath != f.InstallPath {
		t.Fatalf("%+v", f2)
	}
}
