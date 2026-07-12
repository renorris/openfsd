package fsd

import (
	"testing"

	"github.com/renorris/openfsd/pkg/protocol"
)

// Differential tests: fsd thin adapters must match protocol package behavior
// for shared helpers, while getPacketType maps login types to Unknown.

func TestAdapterCountFieldsAndGetField(t *testing.T) {
	packets := [][]byte{
		[]byte(""),
		[]byte("a:b:c"),
		[]byte("@S:GTI8197:2000:1:40.6:-73.7:26:0:1:0\r\n"),
		[]byte("#TMN7938C:@22800:hello\r\n"),
	}
	for _, p := range packets {
		if countFields(p) != protocol.CountFields(p) {
			t.Errorf("countFields mismatch for %q", p)
		}
		// fsd only ever uses non-negative field indices; adapters match protocol for those.
		for i := 0; i < 5; i++ {
			if string(getField(p, i)) != string(protocol.Field(p, i)) {
				t.Errorf("getField(%q,%d) mismatch", p, i)
			}
		}
	}
	// Negative indices: protocol.Field returns nil (safety); fsd getField is the same
	// wrapper. Historical pre-extract getField returned field 0. No fsd call site
	// passes a negative index.
	if getField([]byte("a:b"), -1) != nil {
		t.Error("getField(-1) should be nil via protocol.Field")
	}
}

func TestAdapterGetPacketTypeMapsLoginToUnknown(t *testing.T) {
	// protocol.TypeOf knows these; fsd getPacketType must return Unknown.
	loginPackets := []string{
		"$DISERVER:CLIENT:openfsd:6f70656e667364\r\n",
		"$IDN172SP:SERVER:88e4:vPilot:3:8:1:2:key\r\n",
		"#APN7938C:SERVER:1:t:1:100:0:Name\r\n",
		"#AARN_OBS:SERVER:Name:1:t:1:100\r\n",
		"$ERserver:unknown:4::bad\r\n",
	}
	for _, p := range loginPackets {
		if got := getPacketType([]byte(p)); got != PacketTypeUnknown {
			t.Errorf("getPacketType(%q) = %v, want Unknown", p, got)
		}
		// protocol should classify them non-unknown
		if protocol.TypeOf([]byte(p)) == protocol.PacketTypeUnknown {
			t.Errorf("protocol.TypeOf should know %q", p)
		}
	}

	// Post-login types must still match protocol.TypeOf exactly.
	postLogin := []string{
		"@S:GTI8197:2000:1:40.6:-73.7:26:0:1:0\r\n",
		"%EWR_P_APP:28550:5:150:4:40.67:-74.18:0\r\n",
		"#TMN7938C:@22800:hello\r\n",
		"$CQSRC:DST:RN\r\n",
		"^DAL1151:1:2:3:4:5:6:7:8:9:10:11:12\r\n",
		"#SLX:1:2:3:4:5:6:7:8:9:10:11:12\r\n",
		"#STX:1:2:3:4:5:6\r\n",
		"#DAX:SERVER\r\n",
		"#DPX:SERVER\r\n",
		"#PCX:Y:CCP:BC\r\n",
		"#SBX:Y:P2\r\n",
		"$CRX:Y:RN:Z\r\n",
		"$AXX:Y:METAR:KJFK\r\n",
		"$!!SUP:V:reason\r\n",
		"$ZCX:Y:chal\r\n",
		"$HOX:Y:T\r\n",
		"$HAX:Y:T\r\n",
		"$FPX:Y:rest\r\n",
		"$AMX:Y:T:rest\r\n",
	}
	for _, p := range postLogin {
		got := getPacketType([]byte(p))
		want := protocol.TypeOf([]byte(p))
		if got != want {
			t.Errorf("getPacketType(%q) = %v, protocol.TypeOf = %v", p, got, want)
		}
	}
}

func TestAdapterMinFieldsAndPrefix(t *testing.T) {
	types := []PacketType{
		PacketTypePilotPosition,
		PacketTypePilotPositionFast,
		PacketTypeATCPosition,
		PacketTypeTextMessage,
		PacketTypeFlightPlan,
		PacketTypeUnknown,
	}
	for _, pt := range types {
		if minFields(pt) != protocol.MinFields(pt) {
			t.Errorf("minFields(%v) mismatch", pt)
		}
		if getPacketPrefix(pt) != protocol.Prefix(pt) {
			t.Errorf("getPacketPrefix(%v) mismatch", pt)
		}
	}
}

func TestAdapterSourceCallsign(t *testing.T) {
	pp := []byte("@S:GTI8197:2000:1:40.6:-73.7:26:0:1:0\r\n")
	if string(getSourceCallsign(pp, PacketTypePilotPosition)) != string(protocol.SourceCallsign(pp, PacketTypePilotPosition)) {
		t.Error("getSourceCallsign pilot mismatch")
	}
	if verifySourceCallsign(pp, PacketTypePilotPosition, "GTI8197") !=
		protocol.VerifySourceCallsign(pp, PacketTypePilotPosition, "GTI8197") {
		t.Error("verifySourceCallsign mismatch")
	}
}

func TestNetworkRatingAlias(t *testing.T) {
	if NetworkRatingAdministator != protocol.NetworkRatingAdministator {
		t.Fatal("NetworkRatingAdministator alias broken")
	}
	if NetworkRatingObserver != protocol.NetworkRatingObserver {
		t.Fatal("NetworkRatingObserver alias broken")
	}
	var r NetworkRating = NetworkRatingSupervisor
	var pr protocol.NetworkRating = r
	if pr != protocol.NetworkRatingSupervisor {
		t.Fatal("type alias broken")
	}
}

func TestErrorCodeAlias(t *testing.T) {
	if SyntaxError != int(protocol.SyntaxError) {
		t.Fatal("SyntaxError alias broken")
	}
	if CallsignInUseError != 1 || ClientAuthenticationResponseTimeoutError != 17 {
		t.Fatal("error code range")
	}
}
