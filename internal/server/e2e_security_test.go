package server_test

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/pkg/fsdclient"
	"github.com/renorris/openfsd/pkg/protocol"
)

// TestE2E_MaxSessionsPerCID rejects a 6th concurrent session for the same CID.
func TestE2E_MaxSessionsPerCID(t *testing.T) {
	ts := server.StartTestServerOpts(t, server.TestServerOptions{
		MaxSessionsPerCID: 5,
	})

	// 5 sessions with same pilot CID, different callsigns.
	for i := 0; i < 5; i++ {
		c := dial(t, ts)
		cs := fmt.Sprintf("CIDMX%d", i)
		loginPilot(t, c, cs, ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
		waitMOTD(t, c, cs)
	}

	// 6th must be rejected with ServerFull.
	c6 := dial(t, ts)
	loginPilot(t, c6, "CIDMX5", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitError(t, c6, protocol.ServerFullError)
}

// TestE2E_PositionRatingRewritten ensures peers see session rating, not wire claim.
func TestE2E_PositionRatingRewritten(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginPilot(t, a, "RATE_A", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, a, "RATE_A")

	b := dial(t, ts)
	loginPilot(t, b, "RATE_B", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "RATE_B")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Seed B geo so range search works; A claims forged admin rating on wire.
	forged := nearKJFK("RATE_A")
	forged.NetworkRating = protocol.NetworkRatingAdministator // 12 on wire

	seen := make(chan []byte, 1)
	go func() {
		r, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypePilotPosition &&
				bytes.Contains(r.Raw, []byte("RATE_A"))
		})
		if err != nil {
			return
		}
		seen <- append([]byte(nil), r.Raw...)
	}()

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		_ = b.SendPilotPosition(nearKJFK("RATE_B"))
		if err := a.SendPilotPosition(forged); err != nil {
			t.Fatal(err)
		}
		select {
		case raw := <-seen:
			if bytes.Contains(raw, []byte(":1200:12:")) {
				t.Fatalf("peer saw forged rating: %q", raw)
			}
			// Observer rating is 1.
			if !bytes.Contains(raw, []byte(":1200:1:")) {
				t.Fatalf("expected rewritten rating 1: %q", raw)
			}
			return
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("timeout waiting for position (B=%v)", b.Recorder().All())
		}
	}
}

// TestE2E_PilotCannotDriveATCGeometry drops pilot-originated % packets.
func TestE2E_PilotCannotDriveATCGeometry(t *testing.T) {
	ts := server.StartTestServer(t)

	pilot := dial(t, ts)
	loginPilot(t, pilot, "NOPCT1", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, pilot, "NOPCT1")

	peer := dial(t, ts)
	loginPilot(t, peer, "NOPCT2", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, peer, "NOPCT2")

	exchangeInRangePositions(t, pilot, peer, "NOPCT1", "NOPCT2")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Pilot emits ATC % with huge range (raw wire).
	if err := pilot.Send([]byte("%NOPCT1:28550:0:9999:12:40.64000:-73.78000:0\r\n")); err != nil {
		t.Fatal(err)
	}

	// Peer should not see a % packet from NOPCT1.
	_, err := peer.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeATCPosition && bytes.Contains(r.Raw, []byte("NOPCT1"))
	})
	if err == nil {
		t.Fatal("peer must not receive pilot-originated ATC position")
	}
}

// TestE2E_DeleteLeaveUsesServerBuiltPacket verifies #DP shape on client delete.
func TestE2E_DeleteLeaveUsesServerBuiltPacket(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginPilot(t, a, "LEAVE_A", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, a, "LEAVE_A")

	b := dial(t, ts)
	loginPilot(t, b, "LEAVE_B", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "LEAVE_B")

	exchangeInRangePositions(t, a, b, "LEAVE_A", "LEAVE_B")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cid := strconv.Itoa(ts.PilotCID)
	// verifyPacket requires ≥3 fields; include SERVER (see TestE2E_Disconnect).
	if err := a.Send([]byte("#DPLEAVE_A:SERVER:" + cid + "\r\n")); err != nil {
		t.Fatal(err)
	}

	r, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeDeletePilot &&
			bytes.Contains(r.Raw, []byte("LEAVE_A")) &&
			bytes.Contains(r.Raw, []byte(cid))
	})
	if err != nil {
		t.Fatalf("wait leave: %v (recorder=%v)", err, b.Recorder().All())
	}
	want := "#DPLEAVE_A:SERVER:" + cid
	if !strings.Contains(string(r.Raw), want) {
		t.Fatalf("unexpected leave packet %q want substring %q", r.Raw, want)
	}
}

// TestE2E_MaxConnectionsRejectsExcess opens more TCP clients than allowed.
// On the gnet path, excess connections are closed in OnOpen before $DI, so Dial fails.
func TestE2E_MaxConnectionsRejectsExcess(t *testing.T) {
	ts := server.StartTestServerOpts(t, server.TestServerOptions{
		MaxConnections: 3,
	})

	// Fill 3 connection slots with successful logins.
	for i := 0; i < 3; i++ {
		c := dial(t, ts)
		var cid int
		var pass string
		switch i {
		case 0:
			cid, pass = ts.PilotCID, ts.PilotPassword
		case 1:
			cid, pass = ts.Pilot2CID, ts.PilotPassword
		default:
			cid, pass = ts.SupCID, ts.SupPassword
		}
		cs := fmt.Sprintf("CONMX%d", i)
		loginPilot(t, c, cs, cid, pass, protocol.NetworkRatingObserver)
		waitMOTD(t, c, cs)
	}

	// 4th connection must not complete a normal FSD handshake.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := fsdclient.Dial(ctx, fsdclient.Config{
		Addr:        ts.FSDAddr,
		DialTimeout: 2 * time.Second,
		ReadTimeout: 2 * time.Second,
	})
	if err == nil {
		t.Fatal("expected Dial/handshake failure when at connection cap")
	}
}

// TestE2E_RequirePilotPPL gates pilot #AP logins when REQUIRE_PILOT_PPL is true.
func TestE2E_RequirePilotPPL(t *testing.T) {
	ts := server.StartTestServer(t)

	// Default (false): P0 pilot may connect.
	c := dial(t, ts)
	loginPilot(t, c, "PPLGATE0", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "PPLGATE0")
	_ = c.Close(context.Background())

	// Enable gate.
	if err := ts.ConfigRepo.Set(context.Background(), db.ConfigRequirePilotPPL, "true"); err != nil {
		t.Fatal(err)
	}

	// Pilot still at P0 → rejected.
	c2 := dial(t, ts)
	loginPilot(t, c2, "PPLGATE1", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitError(t, c2, protocol.RequestedLevelTooHighError)

	// Raise pilot certificate to PPL → allowed.
	u, err := ts.UserRepo.GetUserByCID(context.Background(), ts.PilotCID)
	if err != nil {
		t.Fatal(err)
	}
	u.PilotRating = int(protocol.PilotRatingPPL)
	u.Password = "" // keep hash
	if err := ts.UserRepo.UpdateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}

	c3 := dial(t, ts)
	loginPilot(t, c3, "PPLGATE2", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c3, "PPLGATE2")

	// ATC unaffected by the gate (still P0 pilot_rating on ATC user is fine).
	atc := dial(t, ts)
	loginATC(t, atc, "PPL_ATC", ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, "PPL_ATC")
}
