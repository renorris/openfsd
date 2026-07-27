package server_test

import (
	"testing"

	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/pkg/protocol"
)

// TestGnetFSD_LoginAndPosition exercises the production gnet path (StartTestServer
// for dual login, MOTD, and ranged position fan-out.
func TestGnetFSD_LoginAndPosition(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginPilot(t, a, "GNET_A", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, a, "GNET_A")

	b := dial(t, ts)
	loginPilot(t, b, "GNET_B", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "GNET_B")

	// Pump positions until both peers see each other (same helper as classic e2e).
	exchangeInRangePositions(t, a, b, "GNET_A", "GNET_B")
}
