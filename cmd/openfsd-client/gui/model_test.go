package gui

import (
	"path/filepath"
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
		t.Fatal("private host should not match")
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
	if ok || reason == "" {
		t.Fatalf("expected public host block: ok=%v reason=%q", ok, reason)
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
	empty, err := LoadSettings(filepath.Join(dir, "nope.json"))
	if err != nil || empty.ClientID != "" {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}

func TestBuildClientSlots_EmptyWithoutAdapters(t *testing.T) {
	slots := BuildClientSlots(nil)
	if len(slots) != 0 {
		t.Fatalf("expected empty slots without adapters, got %d", len(slots))
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
