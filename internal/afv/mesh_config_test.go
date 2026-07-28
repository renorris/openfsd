package afv

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCluster(t *testing.T) {
	// disabled OK
	if err := (&Config{}).ValidateCluster(); err != nil {
		t.Fatal(err)
	}
	base := &Config{
		ClusterEnabled: true,
		ClusterNodeID:  "n1",
		ClusterListen:  "127.0.0.1:9100",
		ClusterPeers:   "n2=127.0.0.1:9101",
		ClusterPSK:     "psk",
	}
	if err := base.ValidateCluster(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		mut  func(*Config)
		sub  string
	}{
		{"no node", func(c *Config) { c.ClusterNodeID = "" }, "NODE_ID"},
		{"no listen", func(c *Config) { c.ClusterListen = "" }, "LISTEN"},
		{"no psk", func(c *Config) { c.ClusterPSK = "" }, "PSK"},
		{"no peers", func(c *Config) { c.ClusterPeers = "" }, "PEERS"},
		{"self peer", func(c *Config) { c.ClusterPeers = "n1=127.0.0.1:1" }, "self"},
		{"too many", func(c *Config) {
			c.ClusterPeers = "a=1:1,b=1:2,c=1:3,d=1:4,e=1:5"
		}, "max 4"},
		{"bad peer", func(c *Config) { c.ClusterPeers = "notvalid" }, "invalid"},
		{"dup", func(c *Config) { c.ClusterPeers = "n2=1:1,n2=1:2" }, "duplicate"},
		{"bad listen", func(c *Config) { c.ClusterListen = "not-a-hostport" }, "host:port"},
		{"bad peer addr", func(c *Config) { c.ClusterPeers = "n2=nohostport" }, "host:port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := *base
			tc.mut(&c)
			err := c.ValidateCluster()
			if err == nil || !strings.Contains(err.Error(), tc.sub) {
				t.Fatalf("err=%v want sub %q", err, tc.sub)
			}
		})
	}
}

func TestNewDefault_ClusterEnabledFailsClosed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", filepath.Join(dir, "x.db"))
	t.Setenv("DATABASE_AUTO_MIGRATE", "true")
	t.Setenv("AFV_UDP_ADVERTISE_IPV4", "127.0.0.1:50000")
	t.Setenv("AFV_JWT_SECRET", "bootstrap-test-secret-key-material")
	t.Setenv("AFV_CLUSTER_ENABLED", "true")
	t.Setenv("AFV_CLUSTER_NODE_ID", "n1")
	t.Setenv("AFV_CLUSTER_LISTEN", "127.0.0.1:9100")
	t.Setenv("AFV_CLUSTER_PEERS", "n2=127.0.0.1:9101")
	t.Setenv("AFV_CLUSTER_PSK", "psk")
	_, err := NewDefault(context.Background())
	if err == nil || !strings.Contains(err.Error(), "TCP") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewDefault_ClusterIncompleteConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", filepath.Join(dir, "y.db"))
	t.Setenv("DATABASE_AUTO_MIGRATE", "true")
	t.Setenv("AFV_UDP_ADVERTISE_IPV4", "127.0.0.1:50000")
	t.Setenv("AFV_JWT_SECRET", "bootstrap-test-secret-key-material")
	t.Setenv("AFV_CLUSTER_ENABLED", "true")
	// missing peers/psk/etc
	_, err := NewDefault(context.Background())
	if err == nil {
		t.Fatal("expected validate error")
	}
}

func TestParseClusterPeers(t *testing.T) {
	p, err := parseClusterPeers("a=1.2.3.4:5, b=6.7.8.9:10")
	if err != nil || len(p) != 2 || p[0].ID != "a" {
		t.Fatalf("%+v %v", p, err)
	}
}
