package gui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/clientinject"
)

func TestValidateForm_OK(t *testing.T) {
	f := FormState{
		ClientID:    "vpilot",
		InstallPath: "/tmp/vpilot",
		WebBaseURL:  "https://fsd.ex.co",
		FSDHost:     "fsd.ex.co",
	}
	if issues := ValidateForm(f); len(issues) != 0 {
		t.Fatalf("unexpected issues: %+v", issues)
	}
}

func TestValidateForm_Missing(t *testing.T) {
	issues := ValidateForm(FormState{})
	if len(issues) < 3 {
		t.Fatalf("expected multiple issues, got %+v", issues)
	}
}

func TestValidateForm_BadURL(t *testing.T) {
	f := FormState{
		ClientID:    "vpilot",
		InstallPath: "/x",
		WebBaseURL:  "not-a-url",
		FSDHost:     "h",
	}
	issues := ValidateForm(f)
	found := false
	for _, i := range issues {
		if i.Field == "WebBaseURL" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected WebBaseURL issue: %+v", issues)
	}
}

func TestWebBaseHost_AndPublicVATSIM(t *testing.T) {
	if got := WebBaseHost("https://auth.vatsim.net/api"); got != "auth.vatsim.net" {
		t.Fatalf("host=%q", got)
	}
	if !IsPublicVATSIMHost("https://auth.vatsim.net") {
		t.Fatal("expected public VATSIM host")
	}
	if IsPublicVATSIMHost("https://fsd.ex.co") {
		t.Fatal("private host should not warn")
	}
	warn := PublicVATSIMWarning("https://voice1.vatsim.net")
	if warn == "" || !strings.Contains(warn, "voice1.vatsim.net") {
		t.Fatalf("warn=%q", warn)
	}
	if PublicVATSIMWarning("https://openfsd.example.com") != "" {
		t.Fatal("unexpected warning")
	}
}

func TestCanApply(t *testing.T) {
	f := FormState{
		ClientID:    "vpilot",
		InstallPath: "/x",
		WebBaseURL:  "https://fsd.ex.co",
		FSDHost:     "fsd.ex.co",
	}
	ok, _ := CanApply(f, &clientinject.Plan{}, nil)
	if !ok {
		t.Fatal("expected can apply")
	}
	ok, reason := CanApply(f, &clientinject.Plan{Blockers: []string{"nope"}}, nil)
	if ok || reason == "" {
		t.Fatalf("expected blockers: ok=%v reason=%q", ok, reason)
	}
	f.WebBaseURL = "https://auth.vatsim.net"
	ok, reason = CanApply(f, &clientinject.Plan{}, nil)
	if ok || !strings.Contains(reason, "VATSIM") {
		t.Fatalf("expected VATSIM gate: ok=%v reason=%q", ok, reason)
	}
	f.UnderstandPublicVATSIM = true
	ok, _ = CanApply(f, &clientinject.Plan{}, nil)
	if !ok {
		t.Fatal("override should allow")
	}
}

func TestFormatConstraints(t *testing.T) {
	s := FormatConstraints([]clientinject.Constraint{{
		Field: "JWTURL", MaxRunes: 35, Strategy: "in_place", Description: "budget",
	}})
	if !strings.Contains(s, "JWTURL") || !strings.Contains(s, "35") {
		t.Fatalf("%s", s)
	}
	if FormatConstraints(nil) == "" {
		t.Fatal("empty constraints should still return placeholder")
	}
}

func TestFormatFingerprint_Running(t *testing.T) {
	s := FormatFingerprint(clientinject.Install{
		ClientID:  "vpilot",
		RootDir:   "/x",
		PrimaryPE: "/x/vPilot.exe",
		HashSHA1:  "abc",
	}, "vpilot-3.12.1", clientinject.ErrClientRunning)
	if !strings.Contains(s, "running") {
		t.Fatalf("%s", s)
	}
}

func TestParsePortString(t *testing.T) {
	n, err := ParsePortString("6809")
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, err = ParsePortString("6810")
	if err != nil || n != 6810 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if FormatPortString(0) != "6809" {
		t.Fatal(FormatPortString(0))
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	s := Settings{
		ClientID:    "vpilot",
		InstallPath: "/install",
		WebBaseURL:  "https://fsd.ex.co",
		FSDHost:     "fsd.ex.co",
		FSDPort:     6810,
	}
	if err := SaveSettings(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.InstallPath != s.InstallPath || got.WebBaseURL != s.WebBaseURL || got.FSDPort != 6810 {
		t.Fatalf("%+v", got)
	}
	// missing file
	empty, err := LoadSettings(filepath.Join(dir, "nope.json"))
	if err != nil || empty.ClientID != "" {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}

func TestBuildClientSlots(t *testing.T) {
	// Fake adapter map with only vpilot
	type mini struct {
		id, name string
	}
	// Use real engine adapters shape via fake implementing interface is heavy;
	// BuildClientSlots with empty map still returns future catalog.
	slots := BuildClientSlots(nil)
	if len(slots) < 4 {
		t.Fatalf("expected future catalog, got %d", len(slots))
	}
	for _, s := range slots {
		if s.Enabled {
			t.Fatalf("nil adapters should not enable: %+v", s)
		}
		if !strings.Contains(SlotLabel(s), "Coming soon") {
			t.Fatalf("label=%q", SlotLabel(s))
		}
	}
}

func TestFormStateEndpoints(t *testing.T) {
	f := FormState{
		WebBaseURL:         "https://fsd.ex.co/",
		FSDHost:            "fsd.ex.co",
		FSDPort:            6809,
		FSDServerName:      "OPENFSD",
		PreferShortJWTPath: true,
	}
	ep := f.Endpoints()
	if ep.WebBaseURL != "https://fsd.ex.co" {
		t.Fatalf("web=%q", ep.WebBaseURL)
	}
	if ep.JWTURL() != "https://fsd.ex.co/j" {
		t.Fatalf("jwt=%q", ep.JWTURL())
	}
}

func TestLegalConstants(t *testing.T) {
	if LegalApplyBanner == "" || LegalOneLiner == "" {
		t.Fatal("legal text required")
	}
	if !strings.Contains(strings.ToLower(LegalApplyBanner), "private") {
		t.Fatal(LegalApplyBanner)
	}
}
