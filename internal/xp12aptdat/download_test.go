package xp12aptdat

import (
	"strings"
	"testing"
)

func TestParseServerList(t *testing.T) {
	body := `A
1100 version
SERVERS

SERVER desktop.x-plane.com
/unsecured/
CloudFront

MANIFEST X-Plane 12.4.3-r2-15ff1e4d/directory.txt.zip

BRANCH Final
BRANCH_BUILD_NUMBER 124311
BRANCH_NAME X-Plane 12.4.3-r2-15ff1e4d
BRANCH_PATH X-Plane 12.4.3-r2-15ff1e4d

BRANCH Beta
BRANCH_BUILD_NUMBER 124311
BRANCH_NAME X-Plane 12.4.3-r2-15ff1e4d
BRANCH_PATH X-Plane 12.4.3-r2-15ff1e4d

BRANCH RSG
BRANCH_BUILD_NUMBER 120900
BRANCH_NAME X-Plane 12.0.9-dev-1
BRANCH_PATH X-Plane 12.0.9-dev-1

ENDOFLIST
`
	host, prefix, branch, err := ParseServerList(body, "Final")
	if err != nil {
		t.Fatal(err)
	}
	if host != "desktop.x-plane.com" {
		t.Errorf("host = %q", host)
	}
	if prefix != "/unsecured/" {
		t.Errorf("prefix = %q", prefix)
	}
	if branch != "X-Plane 12.4.3-r2-15ff1e4d" {
		t.Errorf("branch = %q", branch)
	}

	_, _, rsg, err := ParseServerList(body, "RSG")
	if err != nil {
		t.Fatal(err)
	}
	if rsg != "X-Plane 12.0.9-dev-1" {
		t.Errorf("RSG branch = %q", rsg)
	}
}

func TestEncodePathKeepSlash(t *testing.T) {
	got := EncodePathKeepSlash("Global Scenery/Global Airports/Earth nav data/apt.dat.zip")
	want := "Global%20Scenery/Global%20Airports/Earth%20nav%20data/apt.dat.zip"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestJoinCDNBase(t *testing.T) {
	base, err := JoinCDNBase("desktop.x-plane.com", "/unsecured/", "X-Plane 12.4.3-r2-15ff1e4d")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(base, "desktop.x-plane.com") {
		t.Errorf("base missing host: %s", base)
	}
	if !strings.Contains(base, "unsecured") {
		t.Errorf("base missing prefix: %s", base)
	}
	if !strings.Contains(base, "X-Plane%2012.4.3") {
		t.Errorf("branch not encoded: %s", base)
	}
	if strings.Contains(base, "X-Plane 12") {
		t.Errorf("raw space in URL: %s", base)
	}

	full, err := JoinURL(base, "Global Scenery/Global Airports/Earth nav data/apt.dat.zip")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(full, "apt.dat.zip") {
		t.Errorf("bad full url: %s", full)
	}
	if strings.Contains(full, " ") {
		t.Errorf("spaces in full url: %s", full)
	}
}
