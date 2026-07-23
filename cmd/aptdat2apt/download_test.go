package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/xp12aptdat"
)

func TestResolveAPTDatWithDownload_NoFlagUsesLocal(t *testing.T) {
	// Without -download, missing local file still errors with guidance that
	// mentions -download.
	_, err := resolveAPTDatWithDownload("", false, "xp12-aptdat", "Final", false, "")
	if err == nil {
		// May succeed if a real apt.dat is on the machine; only check message when missing.
		return
	}
	if !strings.Contains(err.Error(), "-download") {
		t.Fatalf("expected -download hint in error, got: %v", err)
	}
}

func TestResolveAPTDatWithDownload_ExplicitPathWhenNotDownloading(t *testing.T) {
	got, err := resolveAPTDatWithDownload("/tmp/does-not-need-to-exist-yet.dat", false, "", "Final", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/does-not-need-to-exist-yet.dat" {
		t.Fatalf("got %q", got)
	}
}

func TestDownloadTarget(t *testing.T) {
	apt, dir := downloadTarget("", "")
	if dir != xp12aptdat.DefaultOutDir {
		t.Fatalf("default dir = %q", dir)
	}
	if apt != filepath.Join(xp12aptdat.DefaultOutDir, "apt.dat") {
		t.Fatalf("default apt = %q", apt)
	}

	apt, dir = downloadTarget("", "cache/xp")
	if apt != filepath.Join("cache/xp", "apt.dat") || dir != "cache/xp" {
		t.Fatalf("download-dir only: apt=%q dir=%q", apt, dir)
	}

	apt, dir = downloadTarget("/data/custom.apt.dat", "ignored")
	if apt != "/data/custom.apt.dat" || dir != "/data" {
		t.Fatalf("explicit aptdat: apt=%q dir=%q", apt, dir)
	}

	apt, dir = downloadTarget("apt.dat", "ignored")
	if apt != "apt.dat" || dir != "." {
		t.Fatalf("bare filename: apt=%q dir=%q", apt, dir)
	}
}
