package afv_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/afv"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/afvprotocol"
	"github.com/renorris/openfsd/pkg/protocol"
)

func startAFV(t *testing.T, srv *afv.Server) (apiBase, udpAddr string, cancel context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()
	// wait for bind
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		api := srv.APIAddr()
		udp := srv.LocalUDPAddr()
		// Wait until both listeners have bound (not still the :0 request string).
		if api != "" && !strings.HasSuffix(api, ":0") && udp != "" && !strings.HasSuffix(udp, ":0") {
			return "http://" + api, udp, cancel
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	t.Fatal("timeout waiting for AFV bind")
	return "", "", cancel
}

func TestAuth401200(t *testing.T) {
	cfgSecret := []byte("test-jwt-secret-key-32bytes-long!!")
	sqlDB, err := sql.Open("sqlite", "file:afvauth_"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	repos2, err := db.NewRepositories(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	u := &db.User{Password: "s3cret", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	if err := repos2.UserRepo.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	cfg := &afv.Config{
		APIListen:         "127.0.0.1:0",
		UDPListen:         "127.0.0.1:0",
		UDPAdvertiseIPv4:  "127.0.0.1:50000",
		JWTTTL:            time.Hour,
		AuthFailMax:       50,
		AuthFailWindow:    time.Minute,
		MaxDatagram:       8192,
		MaxSessions:       100,
		MaxSessionsPerCID: 5,
	}
	s := afv.New(cfg, repos2.UserRepo, repos2.ConfigRepo, cfgSecret)
	apiBase, _, cancel := startAFV(t, s)
	defer cancel()

	// bad password
	body := []byte(`{"username":"` + strconv.Itoa(u.CID) + `","password":"wrong","client":"test"}`)
	resp, err := http.Post(apiBase+"/api/v1/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", resp.StatusCode)
	}

	// good password
	body = []byte(`{"username":"` + strconv.Itoa(u.CID) + `","password":"s3cret","client":"TrackAudio"}`)
	resp, err = http.Post(apiBase+"/api/v1/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("content-type=%q", ct)
	}
	tok, err := auth.ParseJwtToken(string(raw), cfgSecret)
	if err != nil {
		t.Fatal(err)
	}
	if tok.CustomClaims().TokenType != "afv" || tok.CustomClaims().CID != u.CID {
		t.Fatalf("claims=%+v", tok.CustomClaims())
	}

	// stations
	req, _ := http.NewRequest(http.MethodGet, apiBase+"/api/v1/stations/aliased", nil)
	req.Header.Set("Authorization", "Bearer "+string(raw))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "[]" {
		t.Fatalf("stations status=%d body=%s", resp.StatusCode, b)
	}
}

func TestCallsignReplaceAndTransceivers(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file:afvcs_"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	repos, err := db.NewRepositories(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	u := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	if err := repos.UserRepo.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	secret := []byte("test-jwt-secret-key-32bytes-long!!")
	cfg := &afv.Config{
		APIListen:         "127.0.0.1:0",
		UDPListen:         "127.0.0.1:0",
		UDPAdvertiseIPv4:  "203.0.113.10:50000",
		JWTTTL:            time.Hour,
		MaxSessions:       100,
		MaxSessionsPerCID: 5,
		AuthFailMax:       50,
		AuthFailWindow:    time.Minute,
		MaxDatagram:       8192,
	}
	s := afv.New(cfg, repos.UserRepo, repos.ConfigRepo, secret)
	apiBase, _, cancel := startAFV(t, s)
	defer cancel()

	// auth
	body := []byte(`{"username":"` + strconv.Itoa(u.CID) + `","password":"pw","client":"t"}`)
	resp, err := http.Post(apiBase+"/api/v1/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	tokBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, tokBytes)
	}
	token := string(tokBytes)
	cs := "N123AB"
	path := apiBase + "/api/v1/users/" + strconv.Itoa(u.CID) + "/callsigns/" + cs

	// POST callsign
	req, _ := http.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var pc afv.PostCallsignResponse
	if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("post callsign %d", resp.StatusCode)
	}
	if pc.VoiceServer.AddressIpV4 != "203.0.113.10:50000" {
		t.Fatalf("addr=%s", pc.VoiceServer.AddressIpV4)
	}
	tag1 := pc.VoiceServer.ChannelConfig.ChannelTag
	if tag1 == "" || len(pc.VoiceServer.ChannelConfig.AeadReceiveKey) != 32 {
		t.Fatalf("config=%+v", pc.VoiceServer.ChannelConfig)
	}

	// replace same callsign
	req, _ = http.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var pc2 afv.PostCallsignResponse
	_ = json.NewDecoder(resp.Body).Decode(&pc2)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("replace %d", resp.StatusCode)
	}
	if pc2.VoiceServer.ChannelConfig.ChannelTag == tag1 {
		t.Fatal("replace should rotate channel tag")
	}

	// transceivers
	trxBody := []byte(`[{"ID":0,"Frequency":118700000,"LatDeg":40.64,"LonDeg":-73.78,"HeightMslM":10,"HeightAglM":10}]`)
	req, _ = http.NewRequest(http.MethodPost, path+"/transceivers", bytes.NewReader(trxBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("trx %d", resp.StatusCode)
	}

	// DELETE
	req, _ = http.NewRequest(http.MethodDelete, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("delete %d", resp.StatusCode)
	}
}

func TestCallsignStrict409(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file:afvstrict_"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	repos, err := db.NewRepositories(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	u := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	if err := repos.UserRepo.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	secret := []byte("test-jwt-secret-key-32bytes-long!!")
	cfg := &afv.Config{
		APIListen:         "127.0.0.1:0",
		UDPListen:         "127.0.0.1:0",
		UDPAdvertiseIPv4:  "127.0.0.1:50000",
		JWTTTL:            time.Hour,
		CallsignStrict:    true,
		MaxSessions:       100,
		MaxSessionsPerCID: 5,
		AuthFailMax:       50,
		AuthFailWindow:    time.Minute,
		MaxDatagram:       8192,
	}
	s := afv.New(cfg, repos.UserRepo, repos.ConfigRepo, secret)
	apiBase, _, cancel := startAFV(t, s)
	defer cancel()

	body := []byte(`{"username":"` + strconv.Itoa(u.CID) + `","password":"pw","client":"t"}`)
	resp, _ := http.Post(apiBase+"/api/v1/auth", "application/json", bytes.NewReader(body))
	tok, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	path := apiBase + "/api/v1/users/" + strconv.Itoa(u.CID) + "/callsigns/TEST1"
	req, _ := http.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+string(tok))
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("first %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+string(tok))
	resp, _ = http.DefaultClient.Do(req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("strict want 409 got %d", resp.StatusCode)
	}
}

func TestUDPHAandA2A(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file:afvudp_"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(sqlDB); err != nil {
		t.Fatal(err)
	}
	repos, err := db.NewRepositories(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	u1 := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	u2 := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	if err := repos.UserRepo.CreateUser(context.Background(), u1); err != nil {
		t.Fatal(err)
	}
	if err := repos.UserRepo.CreateUser(context.Background(), u2); err != nil {
		t.Fatal(err)
	}
	secret := []byte("test-jwt-secret-key-32bytes-long!!")
	cfg := &afv.Config{
		APIListen:          "127.0.0.1:0",
		UDPListen:          "127.0.0.1:0",
		UDPAdvertiseIPv4:   "127.0.0.1:0",
		JWTTTL:             time.Hour,
		MaxSessions:        100,
		MaxSessionsPerCID:  5,
		AuthFailMax:        50,
		AuthFailWindow:     time.Minute,
		MaxDatagram:        8192,
		RangeDefaultNM:     100,
		RangeEdgeRatio:     0.1,
		HeartbeatTimeout:   10 * time.Second,
		SessionIdleTimeout: 30 * time.Second,
	}
	s := afv.New(cfg, repos.UserRepo, repos.ConfigRepo, secret)
	apiBase, udpAddr, cancel := startAFV(t, s)
	defer cancel()
	// Fix advertise to actual UDP
	cfg.UDPAdvertiseIPv4 = udpAddr

	authTok := func(cid int) string {
		body := []byte(`{"username":"` + strconv.Itoa(cid) + `","password":"pw","client":"t"}`)
		resp, err := http.Post(apiBase+"/api/v1/auth", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("auth %d %s", resp.StatusCode, b)
		}
		return string(b)
	}
	postCS := func(cid int, token, cs string) afv.PostCallsignResponse {
		path := apiBase + "/api/v1/users/" + strconv.Itoa(cid) + "/callsigns/" + cs
		req, _ := http.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var pc afv.PostCallsignResponse
		if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("post cs %d", resp.StatusCode)
		}
		// force advertise actual
		pc.VoiceServer.AddressIpV4 = udpAddr
		return pc
	}
	postTrx := func(cid int, token, cs string, lat, lon float64, freq uint32) {
		path := apiBase + "/api/v1/users/" + strconv.Itoa(cid) + "/callsigns/" + cs + "/transceivers"
		body, _ := json.Marshal([]afv.Transceiver{{
			ID: 0, Frequency: freq, LatDeg: lat, LonDeg: lon, HeightMslM: 100, HeightAglM: 100,
		}})
		req, _ := http.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("trx %d", resp.StatusCode)
		}
	}

	tok1 := authTok(u1.CID)
	tok2 := authTok(u2.CID)
	pc1 := postCS(u1.CID, tok1, "AAL1")
	pc2 := postCS(u2.CID, tok2, "AAL2")
	const freq uint32 = 122800000 // UNICOM 15NM
	// both at KJFK-ish, close
	postTrx(u1.CID, tok1, "AAL1", 40.64, -73.78, freq)
	postTrx(u2.CID, tok2, "AAL2", 40.641, -73.781, freq)

	udpResolved, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		t.Fatal(err)
	}

	// Client 1: send H, expect HA
	cli1, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer cli1.Close()
	cli2, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer cli2.Close()

	ch1, err := afvprotocol.ClientChannel(
		pc1.VoiceServer.ChannelConfig.ChannelTag,
		pc1.VoiceServer.ChannelConfig.AeadReceiveKey,
		pc1.VoiceServer.ChannelConfig.AeadTransmitKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	ch2, err := afvprotocol.ClientChannel(
		pc2.VoiceServer.ChannelConfig.ChannelTag,
		pc2.VoiceServer.ChannelConfig.AeadReceiveKey,
		pc2.VoiceServer.ChannelConfig.AeadTransmitKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Heartbeat from client 1
	hbPkt, err := ch1.Encapsulate(0, afvprotocol.DTONameHeartbeat, afvprotocol.Heartbeat{Callsign: "AAL1"}.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli1.WriteTo(hbPkt, udpResolved); err != nil {
		t.Fatal(err)
	}
	_ = cli1.SetReadDeadline(time.Now().Add(2 * time.Second))
	rbuf := make([]byte, 8192)
	n, _, err := cli1.ReadFrom(rbuf)
	if err != nil {
		t.Fatalf("HA read: %v", err)
	}
	_, name, payload, err := ch1.Decapsulate(rbuf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if name != "HA" || len(payload) != 0 {
		t.Fatalf("want HA empty payload, got %s len=%d", name, len(payload))
	}

	// Bind client 2 with heartbeat
	hb2, _ := ch2.Encapsulate(0, afvprotocol.DTONameHeartbeat, afvprotocol.Heartbeat{Callsign: "AAL2"}.EncodeMsgpack(), nil)
	_, _ = cli2.WriteTo(hb2, udpResolved)
	_ = cli2.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err = cli2.ReadFrom(rbuf)
	if err != nil {
		t.Fatalf("cli2 HA: %v", err)
	}
	_, name, _, err = ch2.Decapsulate(rbuf[:n])
	if err != nil || name != "HA" {
		t.Fatalf("cli2 name=%s err=%v", name, err)
	}

	// A2A: client1 AT → client2 AR
	at := afvprotocol.AudioTx{
		Callsign:        "AAL1",
		SequenceCounter: 1,
		Audio:           []byte{9, 8, 7, 6, 5},
		LastPacket:      true,
		Transceivers:    []afvprotocol.TxTransceiver{{ID: 0}},
	}
	atPkt, err := ch1.Encapsulate(1, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli1.WriteTo(atPkt, udpResolved); err != nil {
		t.Fatal(err)
	}
	_ = cli2.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err = cli2.ReadFrom(rbuf)
	if err != nil {
		t.Fatalf("AR read: %v", err)
	}
	_, name, payload, err = ch2.Decapsulate(rbuf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if name != "AR" {
		t.Fatalf("want AR got %s", name)
	}
	ar, err := afvprotocol.DecodeAudioRx(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ar.Callsign != "AAL1" || !bytes.Equal(ar.Audio, at.Audio) {
		t.Fatalf("AR=%+v", ar)
	}
	if len(ar.Transceivers) == 0 {
		t.Fatal("expected RX transceiver entries")
	}
}
