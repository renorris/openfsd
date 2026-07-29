package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/clientinject"
)

func TestRun_HelpNoArgs(t *testing.T) {
	var out, err bytes.Buffer
	code := Run(nil, &out, &err)
	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, err.String())
	}
	if !strings.Contains(out.String(), "openfsd-client") {
		t.Fatalf("help missing: %s", out.String())
	}
	if !strings.Contains(out.String(), "max host 12") {
		t.Fatal("expected readiness honesty in help")
	}
}

func TestRun_UnknownSubcommand(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"nope"}, &out, &err)
	if code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestRun_ListProfiles(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"list-profiles"}, &out, &err)
	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, err.String())
	}
	if !strings.Contains(out.String(), "vpilot-3.12.1") {
		t.Fatalf("expected embedded profile: %s", out.String())
	}
	if strings.Contains(out.String(), "xpilot") {
		t.Fatalf("xpilot must not appear: %s", out.String())
	}
}

func TestRun_PlanUsageMissingInstall(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"plan", "--web-base", "https://fsd.ex.co", "--fsd-host", "fsd.ex.co"}, &out, &err)
	if code != ExitUsage {
		t.Fatalf("code=%d err=%s", code, err.String())
	}
}

func TestRun_HelpFlag(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"--help"}, &out, &err)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
}

func TestRun_Detect(t *testing.T) {
	var out, err bytes.Buffer
	code := Run([]string{"detect", "--client", "vpilot"}, &out, &err)
	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, err.String())
	}
	// May find zero installs on this machine; just ensure no crash.
}

func TestRun_HelpMentionsEphemeral(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := Run([]string{"help"}, &out, &errBuf)
	if code != ExitOK {
		t.Fatalf("code=%d err=%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "--ephemeral") {
		t.Fatalf("help missing --ephemeral: %s", out.String())
	}
	if !strings.Contains(out.String(), "7 client process") {
		t.Fatalf("help missing exit 7: %s", out.String())
	}
}

func TestMapLaunchErr_ProcessExit(t *testing.T) {
	var errBuf bytes.Buffer
	code := mapLaunchErr(&clientinject.ProcessExitError{Name: "vPilot.exe", ExitCode: 2}, &errBuf)
	if code != ExitClientProcess {
		t.Fatalf("code=%d want %d err=%s", code, ExitClientProcess, errBuf.String())
	}
}

func TestRun_LaunchDryPrintWithoutEphemeral(t *testing.T) {
	// Missing install → usage; ensures flag parsing accepts launch without ephemeral.
	var out, errBuf bytes.Buffer
	code := Run([]string{"launch", "--web-base", "https://x.test", "--fsd-host", "x.test"}, &out, &errBuf)
	if code != ExitUsage {
		t.Fatalf("code=%d err=%s", code, errBuf.String())
	}
}
