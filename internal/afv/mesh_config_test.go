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
		ClusterEnabled:     true,
		ClusterNodeID:      "n1",
		ClusterListen:      "127.0.0.1:9100",
		ClusterVoiceListen: "127.0.0.1:9101",
		ClusterPeers:       "n2=127.0.0.1:9110/9111",
		ClusterPSK:         "psk",
		UDPListen:          "127.0.0.1:50000",
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
		{"no voice", func(c *Config) { c.ClusterVoiceListen = "" }, "VOICE_LISTEN"},
		{"no psk", func(c *Config) { c.ClusterPSK = "" }, "PSK"},
		{"no peers", func(c *Config) { c.ClusterPeers = "" }, "PEERS"},
		{"self peer", func(c *Config) { c.ClusterPeers = "n1=127.0.0.1:9110/9111" }, "self"},
		{"too many", func(c *Config) {
			c.ClusterPeers = "a=1.1.1.1:1/2,b=1.1.1.1:3/4,c=1.1.1.1:5/6,d=1.1.1.1:7/8,e=1.1.1.1:9/10"
		}, "max 4"},
		{"bad peer", func(c *Config) { c.ClusterPeers = "notvalid" }, "invalid"},
		{"dup", func(c *Config) { c.ClusterPeers = "n2=1.1.1.1:1/2,n2=1.1.1.1:3/4" }, "duplicate"},
		{"bad listen", func(c *Config) { c.ClusterListen = "not-a-hostport" }, "host:port"},
		{"bad peer addr", func(c *Config) { c.ClusterPeers = "n2=nohostport" }, "host:port"},
		{"voice same port as tcp", func(c *Config) { c.ClusterVoiceListen = "127.0.0.1:9100" }, "differ"},
		{"voice equals peer", func(c *Config) {
			c.ClusterVoiceListen = "127.0.0.1:9111"
		}, "VoiceAddr"},
		{"voice equals udp", func(c *Config) {
			c.ClusterVoiceListen = "127.0.0.1:50000"
		}, "UDP_LISTEN"},
		{"voice port 0", func(c *Config) { c.ClusterVoiceListen = "127.0.0.1:0" }, "port"},
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

func TestValidateCluster_MultiHostSameVoicePortOK(t *testing.T) {
	// H-20 rev3: local 0.0.0.0:17001 + peer 10.0.0.2:17001 is OK
	c := &Config{
		ClusterEnabled:     true,
		ClusterNodeID:      "n1",
		ClusterListen:      "0.0.0.0:17000",
		ClusterVoiceListen: "0.0.0.0:17001",
		ClusterPeers:       "n2=10.0.0.2:17000", // voice derives 10.0.0.2:17001
		ClusterPSK:         "psk",
		UDPListen:          "0.0.0.0:50000",
	}
	if err := c.ValidateCluster(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCluster_DupVoiceAddr(t *testing.T) {
	c := &Config{
		ClusterEnabled:     true,
		ClusterNodeID:      "n1",
		ClusterListen:      "0.0.0.0:17000",
		ClusterVoiceListen: "0.0.0.0:17001",
		// Same VoiceAddr host:port on two peers (H-20 unique VoiceAddr)
		ClusterPeers: "n2=10.0.0.2:17000/17001,n3=10.0.0.2:17010/17001",
		ClusterPSK:   "psk",
		UDPListen:    "0.0.0.0:50000",
	}
	err := c.ValidateCluster()
	if err == nil || !strings.Contains(err.Error(), "duplicate peer VoiceAddr") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseClusterPeers(t *testing.T) {
	fixtures := []struct {
		in        string
		wantID    string
		wantAddr  string
		wantVoice string
		wantErr   string
	}{
		{"n2=10.0.0.2:17000", "n2", "10.0.0.2:17000", "10.0.0.2:17001", ""},
		{"n2=10.0.0.2:17000/17100", "n2", "10.0.0.2:17000", "10.0.0.2:17100", ""},
		{"n2=[::1]:17000", "n2", "[::1]:17000", "[::1]:17001", ""},
		{"n2=[::1]:17000/17050", "n2", "[::1]:17000", "[::1]:17050", ""},
		{" n2 = 10.0.0.2:17000/ 17100 ", "n2", "10.0.0.2:17000", "10.0.0.2:17100", ""},
		{"n2=10.0.0.2:65535", "", "", "", "overflow"},
		{"n2=10.0.0.2:17000/", "", "", "", "empty voice"},
		{"n2=10.0.0.2:17000/abc", "", "", "", "non-numeric"},
		{"n2=10.0.0.2:17000/0", "", "", "", "port 0"},
		{"n2=10.0.0.2:0", "", "", "", "port 0"},
		{"n2=::1:17000", "", "", "", "host:port"},
	}
	for _, tc := range fixtures {
		t.Run(tc.in, func(t *testing.T) {
			p, err := parseClusterPeers(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || len(p) != 1 {
				t.Fatalf("p=%+v err=%v", p, err)
			}
			if p[0].ID != tc.wantID || p[0].Addr != tc.wantAddr || p[0].VoiceAddr != tc.wantVoice {
				t.Fatalf("got %+v want id=%s addr=%s voice=%s", p[0], tc.wantID, tc.wantAddr, tc.wantVoice)
			}
		})
	}

	// multi
	p, err := parseClusterPeers("a=1.2.3.4:5, b=6.7.8.9:10")
	if err != nil || len(p) != 2 || p[0].ID != "a" || p[0].VoiceAddr != "1.2.3.4:6" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestNewDefault_ClusterEnabledWiresHybrid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", filepath.Join(dir, "x.db"))
	t.Setenv("DATABASE_AUTO_MIGRATE", "true")
	t.Setenv("AFV_UDP_ADVERTISE_IPV4", "127.0.0.1:50000")
	t.Setenv("AFV_UDP_LISTEN", "127.0.0.1:50000")
	t.Setenv("AFV_JWT_SECRET", "bootstrap-test-secret-key-material")
	t.Setenv("AFV_CLUSTER_ENABLED", "true")
	t.Setenv("AFV_CLUSTER_NODE_ID", "n1")
	t.Setenv("AFV_CLUSTER_LISTEN", "127.0.0.1:19100")
	t.Setenv("AFV_CLUSTER_VOICE_LISTEN", "127.0.0.1:19101")
	t.Setenv("AFV_CLUSTER_PEERS", "n2=127.0.0.1:19110/19111")
	t.Setenv("AFV_CLUSTER_PSK", "psk")
	s, err := NewDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Mesh() == nil {
		t.Fatal("expected non-nil Mesh")
	}
	if _, ok := s.Mesh().(*HybridMesh); !ok {
		t.Fatalf("want *HybridMesh got %T", s.Mesh())
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

func TestNewDefault_ClusterDisabledNilMesh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATABASE_DRIVER", "sqlite")
	t.Setenv("DATABASE_SOURCE_NAME", filepath.Join(dir, "z.db"))
	t.Setenv("DATABASE_AUTO_MIGRATE", "true")
	t.Setenv("AFV_UDP_ADVERTISE_IPV4", "127.0.0.1:50000")
	t.Setenv("AFV_JWT_SECRET", "bootstrap-test-secret-key-material")
	t.Setenv("AFV_CLUSTER_ENABLED", "false")
	s, err := NewDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Mesh() != nil {
		t.Fatal("expected nil mesh when disabled")
	}
}
