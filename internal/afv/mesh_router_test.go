package afv

import (
	"testing"
	"time"
)

func TestRouteSyntheticTX_ATCVsPilot(t *testing.T) {
	cfg := &Config{
		MaxSessions: 10, MaxSessionsPerCID: 5,
		RangeDefaultNM: 40, RangeATCNM: 150, RangeEdgeRatio: 0.1,
	}
	r := newRegistry(cfg)
	now := time.Now()
	// pilot RX ~100 NM from TX
	rx, _, err := r.CreateOrReplace(1, "AAL1", "", now)
	if err != nil {
		t.Fatal(err)
	}
	// ~1.5 deg lat ≈ 90 NM
	if _, _, err := r.UpdateTransceivers(1, "AAL1", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 41.5, LonDeg: -73.0, HeightMslM: 100,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := r.BindUDP(rx, fakeAddr{"127.0.0.1:9"}, now); !ok {
		t.Fatal("bind")
	}

	txRadios := []RelayTxRadio{{
		TxID: 0, FreqHz: 118700000, LatDeg: 40.0, LonDeg: -73.0, HeightM: 100,
	}}

	// pilot TX class → Default 40NM → no recipient
	recs := r.routeSyntheticTX("JFK_TWR", false, txRadios)
	if len(recs) != 0 {
		t.Fatalf("pilot TX at ~90NM should miss, got %d", len(recs))
	}
	// ATC TX → 150NM → recipient
	recs = r.routeSyntheticTX("JFK_TWR", true, txRadios)
	if len(recs) != 1 {
		t.Fatalf("ATC TX should reach pilot, got %d", len(recs))
	}
}

func TestRouteSyntheticTX_DualLoginSkip(t *testing.T) {
	cfg := &Config{MaxSessions: 10, MaxSessionsPerCID: 5, RangeDefaultNM: 100}
	r := newRegistry(cfg)
	now := time.Now()
	rx, _, _ := r.CreateOrReplace(1, "AAL1", "", now)
	_, _, _ = r.UpdateTransceivers(1, "AAL1", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40.0, LonDeg: -73.0,
	}})
	_, _, _ = r.BindUDP(rx, fakeAddr{"1"}, now)

	// same callsign as local → skip
	recs := r.routeSyntheticTX("AAL1", false, []RelayTxRadio{{
		TxID: 0, FreqHz: 118700000, LatDeg: 40.001, LonDeg: -73.001,
	}})
	if len(recs) != 0 {
		t.Fatalf("dual-login should skip, got %d", len(recs))
	}
}

func TestRouteSyntheticTX_IsXCNoReXC(t *testing.T) {
	// isXC: primary synthetic TX only — never XC-again (PR-9 not implemented).
	// routeSyntheticTX is frequency-primary; second freq is not auto-coupled.
	cfg := &Config{MaxSessions: 10, MaxSessionsPerCID: 5, RangeDefaultNM: 100}
	r := newRegistry(cfg)
	now := time.Now()
	rx, _, _ := r.CreateOrReplace(2, "AAL2", "", now)
	_, _, _ = r.UpdateTransceivers(2, "AAL2", []Transceiver{{
		ID: 0, Frequency: 118700000, LatDeg: 40.0, LonDeg: -73.0,
	}})
	_, _, _ = r.BindUDP(rx, fakeAddr{"2"}, now)
	recs := r.routeSyntheticTX("AAL1", false, []RelayTxRadio{
		{TxID: 0, FreqHz: 118700000, LatDeg: 40.01, LonDeg: -73.01},
		{TxID: 1, FreqHz: 119000000, LatDeg: 40.01, LonDeg: -73.01}, // no local RX
	})
	if len(recs) != 1 {
		t.Fatalf("primary-only recipients=%d", len(recs))
	}
	// handleMeshAudioRelay with IsXC=true: no panic, primary route only (nil udp drops)
	s := New(cfg, nil, nil, []byte("k"))
	s.reg = r
	s.handleMeshAudioRelay("nX", AudioRelay{
		Callsign: "AAL1", IsATC: false, IsXC: true, Audio: []byte{1},
		TxRadios: []RelayTxRadio{
			{TxID: 0, FreqHz: 118700000, LatDeg: 40.01, LonDeg: -73.01},
			{TxID: 1, FreqHz: 119000000, LatDeg: 40.01, LonDeg: -73.01},
		},
	})
}

func TestHandleMeshAudioRelay_NilUDP(t *testing.T) {
	cfg := &Config{MaxSessions: 10, MaxSessionsPerCID: 5, RangeDefaultNM: 100}
	s := New(cfg, nil, nil, []byte("k"))
	// no udpConn
	s.handleMeshAudioRelay("n2", AudioRelay{
		Callsign: "X", IsATC: false, Audio: []byte{1},
		TxRadios: []RelayTxRadio{{FreqHz: 118700000, LatDeg: 0, LonDeg: 0}},
	})
	// must not panic
}
