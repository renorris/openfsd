package afv

import (
	"net"
	"testing"
	"time"

	"github.com/renorris/openfsd/pkg/afvprotocol"
)

type fakeAddr struct{ s string }

func (f fakeAddr) Network() string { return "udp" }
func (f fakeAddr) String() string  { return f.s }

func TestRouteAT_A2A(t *testing.T) {
	cfg := &Config{
		MaxSessions:       10,
		MaxSessionsPerCID: 5,
		RangeDefaultNM:    100,
		RangeEdgeRatio:    0.1,
	}
	r := newRegistry(cfg)
	now := time.Now()
	tx, err := r.CreateOrReplace(1, "AAL1", "", now)
	if err != nil {
		t.Fatal(err)
	}
	rx, err := r.CreateOrReplace(2, "AAL2", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateTransceivers(1, "AAL1", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40.0, LonDeg: -73.0, HeightMslM: 100,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateTransceivers(2, "AAL2", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40.01, LonDeg: -73.01, HeightMslM: 100,
	}}); err != nil {
		t.Fatal(err)
	}
	// bind both
	if _, ok := r.BindUDP(tx, fakeAddr{"127.0.0.1:1"}, now); !ok {
		t.Fatal("bind tx")
	}
	if _, ok := r.BindUDP(rx, fakeAddr{"127.0.0.1:2"}, now); !ok {
		t.Fatal("bind rx")
	}

	recs := r.routeAT(tx, afvprotocol.AudioTx{
		Callsign:     "AAL1",
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	})
	if len(recs) != 1 {
		t.Fatalf("recipients=%d", len(recs))
	}
	if recs[0].sess != rx {
		t.Fatal("wrong recipient")
	}
	if len(recs[0].rx) != 1 || recs[0].rx[0].DistanceRatio <= 0 {
		t.Fatalf("rx entries=%+v", recs[0].rx)
	}

	// no self
	self := r.routeAT(tx, afvprotocol.AudioTx{
		Callsign:     "AAL1",
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	})
	for _, rec := range self {
		if rec.sess == tx {
			t.Fatal("hear self")
		}
	}
}

func TestCallsignMatch(t *testing.T) {
	if !callsignMatch("aal1", "AAL1") {
		t.Fatal()
	}
	if callsignMatch("a", "b") {
		t.Fatal()
	}
}

// ensure net.Addr compile
var _ net.Addr = fakeAddr{}
