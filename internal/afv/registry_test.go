package afv

import (
	"testing"
	"time"
)

func TestRegistryReplaceAndLimits(t *testing.T) {
	cfg := &Config{MaxSessions: 2, MaxSessionsPerCID: 1, CallsignStrict: false}
	r := newRegistry(cfg)
	now := time.Now()
	s1, err := r.CreateOrReplace(1, "AAA", "", now)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := r.CreateOrReplace(1, "AAA", "", now) // replace same callsign frees then re-adds
	if err != nil {
		t.Fatal(err)
	}
	if s1.ChannelTag == s2.ChannelTag {
		t.Fatal("tag should rotate")
	}
	// second callsign same CID → per-CID limit
	if _, err := r.CreateOrReplace(1, "BBB", "", now); err != errCIDSessionLimit {
		t.Fatalf("err=%v", err)
	}
	// other CID
	if _, err := r.CreateOrReplace(2, "CCC", "", now); err != nil {
		t.Fatal(err)
	}
	// global limit
	if _, err := r.CreateOrReplace(3, "DDD", "", now); err != errSessionLimit {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryStrict(t *testing.T) {
	cfg := &Config{MaxSessions: 10, MaxSessionsPerCID: 5, CallsignStrict: true}
	r := newRegistry(cfg)
	now := time.Now()
	if _, err := r.CreateOrReplace(1, "X", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateOrReplace(2, "X", "", now); err != errCallsignInUse {
		t.Fatalf("err=%v", err)
	}
}

func TestRegistryTransceiversAndReap(t *testing.T) {
	cfg := &Config{
		MaxSessions:        10,
		MaxSessionsPerCID:  5,
		HeartbeatTimeout:   50 * time.Millisecond,
		SessionIdleTimeout: 50 * time.Millisecond,
	}
	r := newRegistry(cfg)
	now := time.Now()
	s, err := r.CreateOrReplace(1, "PILOT", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateTransceivers(1, "PILOT", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40, LonDeg: -70,
	}}); err != nil {
		t.Fatal(err)
	}
	if !s.IsATC == false && len(s.Transceivers) != 1 {
		// 1 trx no underscore → not ATC
	}
	if s.IsATC {
		t.Fatal("single trx pilot should not be ATC")
	}
	// unbound idle reap
	time.Sleep(60 * time.Millisecond)
	n := r.Reap(time.Now())
	if n != 1 {
		t.Fatalf("reaped %d", n)
	}
	if r.Count() != 0 {
		t.Fatal()
	}
}
