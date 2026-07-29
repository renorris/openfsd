package clientinject

import "testing"

func TestEndpoints_JWTURL(t *testing.T) {
	ep := Endpoints{WebBaseURL: "https://fsd.example.com/"}
	if got := ep.JWTURL(); got != "https://fsd.example.com/api/v1/fsd-jwt" {
		t.Fatalf("JWTURL=%q", got)
	}
	ep.PreferShortJWTPath = true
	if got := ep.JWTURL(); got != "https://fsd.example.com/j" {
		t.Fatalf("short JWTURL=%q", got)
	}
	ep.ShortJWTPathCandidates = []string{"/api/fsd-jwt"}
	if got := ep.JWTURL(); got != "https://fsd.example.com/api/fsd-jwt" {
		t.Fatalf("custom short=%q", got)
	}
	if (Endpoints{}).JWTURL() != "" {
		t.Fatal("empty base")
	}
}

func TestEndpoints_StatusURL(t *testing.T) {
	ep := Endpoints{WebBaseURL: "https://x.test"}
	if got := ep.StatusURL(); got != "https://x.test/api/v1/data/status.txt" {
		t.Fatalf("StatusURL=%q", got)
	}
	if got := ep.StatusJSONURL(); got != "https://x.test/api/v1/data/status.json" {
		t.Fatalf("StatusJSON=%q", got)
	}
}

func TestEndpoints_FSDAddress(t *testing.T) {
	ep := Endpoints{FSDHost: "fsd.example.com"}
	if got := ep.FSDAddress(); got != "fsd.example.com" {
		t.Fatalf("default port omit: %q", got)
	}
	ep.FSDPort = 6809
	if got := ep.FSDAddress(); got != "fsd.example.com" {
		t.Fatalf("explicit default: %q", got)
	}
	ep.IncludePortInServerList = true
	if got := ep.FSDAddress(); got != "fsd.example.com:6809" {
		t.Fatalf("include port: %q", got)
	}
	ep.IncludePortInServerList = false
	ep.FSDPort = 7000
	if got := ep.FSDAddress(); got != "fsd.example.com:7000" {
		t.Fatalf("nondefault: %q", got)
	}
	ep.FSDHost = "fsd.example.com:7001"
	ep.FSDPort = 0
	if got := ep.FSDAddress(); got != "fsd.example.com:7001" {
		t.Fatalf("host already has port: %q", got)
	}
}

func TestEndpoints_CachedServerEntry(t *testing.T) {
	ep := Endpoints{FSDHost: "h", FSDServerName: "OPENFSD"}
	if got := ep.CachedServerEntry(); got != "OPENFSD|h" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultShortJWTPathCandidates(t *testing.T) {
	c := DefaultShortJWTPathCandidates()
	if len(c) < 3 || c[0] != "/j" {
		t.Fatalf("%v", c)
	}
}
