package protocol

import "testing"

func TestTypeOf(t *testing.T) {
	tests := []struct {
		packet string
		want   PacketType
	}{
		{"", PacketTypeUnknown},
		{"x", PacketTypeUnknown},
		{"@", PacketTypePilotPosition},
		{"@S:GTI8197:2000:1:40.65906:-73.79891:26:0:4290776072:359", PacketTypePilotPosition},
		{"^DAL1151:40.6:-73.7:16:8:1:0:0:0:0:0:0:0", PacketTypePilotPositionFast},
		{"%EWR_P_APP:28550:5:150:4:40.67:-74.18:0", PacketTypeATCPosition},
		{"'LAX_CTR:0:34.05200:-118.24300", PacketTypeSecondaryVisCenter},
		{"#DAOBS:SERVER", PacketTypeDeleteATC},
		{"#DPN123:SERVER", PacketTypeDeletePilot},
		{"#TMN7938C:@22800:hi", PacketTypeTextMessage},
		{"#SLPRM4211:1:2:3:4:5:6:7:8:9:10:11:12", PacketTypePilotPositionSlow},
		{"#STDAL2119:1:2:3:4:5:6", PacketTypePilotPositionStopped},
		{"#PCSRC:DST:CCP:BC", PacketTypeProController},
		{"#SBSRC:DST:P2", PacketTypeSquawkbox},
		{"#APN7938C:SERVER:1:t:1:100:0:Name", PacketTypeAddPilot},
		{"#AARN_OBS:SERVER:Name:1:t:1:100", PacketTypeAddATC},
		{"#XX", PacketTypeUnknown},
		{"#", PacketTypeUnknown},
		{"#A", PacketTypeUnknown},
		{"$CQSRC:DST:RN", PacketTypeClientQuery},
		{"$CRSRC:DST:RN:Name", PacketTypeClientQueryResponse},
		{"$AXSRC:DST:METAR:KJFK", PacketTypeMetarRequest},
		{"$!!SUP:VICTIM:reason", PacketTypeKillRequest},
		{"$ZCSRC:DST:challenge", PacketTypeAuthChallenge},
		{"$HOSRC:DST:TGT", PacketTypeHandoffRequest},
		{"$HASRC:DST:TGT", PacketTypeHandoffAccept},
		{"$HCSRC:DST:TGT", PacketTypeHandoffCancel},
		{"$FPSRC:DST:fields", PacketTypeFlightPlan},
		{"$AMSRC:DST:TGT:fields", PacketTypeFlightPlanAmendment},
		{"$DISERVER:CLIENT:openfsd:6f70656e667364", PacketTypeServerIdent},
		{"$IDN172SP:SERVER:88e4:vPilot:3:8:1:2:key", PacketTypeClientIdent},
		{"$ERserver:unknown:4::bad", PacketTypeError},
		{"$ZZ", PacketTypeUnknown},
		{"$", PacketTypeUnknown},
		{"$A", PacketTypeUnknown},
	}
	for _, tt := range tests {
		got := TypeOf([]byte(tt.packet))
		if got != tt.want {
			t.Errorf("TypeOf(%q) = %v, want %v", tt.packet, got, tt.want)
		}
	}
}

func TestPrefix(t *testing.T) {
	tests := []struct {
		t    PacketType
		want string
	}{
		{PacketTypePilotPositionFast, "^"},
		{PacketTypePilotPosition, "@"},
		{PacketTypeATCPosition, "%"},
		{PacketTypeSecondaryVisCenter, "'"},
		{PacketTypeDeleteATC, "#DA"},
		{PacketTypeDeletePilot, "#DP"},
		{PacketTypeTextMessage, "#TM"},
		{PacketTypePilotPositionSlow, "#SL"},
		{PacketTypePilotPositionStopped, "#ST"},
		{PacketTypeProController, "#PC"},
		{PacketTypeSquawkbox, "#SB"},
		{PacketTypeClientQuery, "$CQ"},
		{PacketTypeClientQueryResponse, "$CR"},
		{PacketTypeMetarRequest, "$AX"},
		{PacketTypeKillRequest, "$!!"},
		{PacketTypeAuthChallenge, "$ZC"},
		{PacketTypeHandoffRequest, "$HO"},
		{PacketTypeHandoffAccept, "$HA"},
		{PacketTypeHandoffCancel, "$HC"},
		{PacketTypeFlightPlan, "$FP"},
		{PacketTypeFlightPlanAmendment, "$AM"},
		{PacketTypeServerIdent, "$DI"},
		{PacketTypeClientIdent, "$ID"},
		{PacketTypeAddPilot, "#AP"},
		{PacketTypeAddATC, "#AA"},
		{PacketTypeError, "$ER"},
		{PacketTypeUnknown, ""},
		{PacketType(999), ""},
	}
	for _, tt := range tests {
		got := Prefix(tt.t)
		if got != tt.want {
			t.Errorf("Prefix(%v) = %q, want %q", tt.t, got, tt.want)
		}
	}
}

func TestMinFields(t *testing.T) {
	// Historical fsd values for post-login types must be preserved.
	tests := []struct {
		t    PacketType
		want int
	}{
		{PacketTypePilotPosition, 9},
		{PacketTypePilotPositionFast, 13},
		{PacketTypePilotPositionSlow, 13},
		{PacketTypePilotPositionStopped, 7},
		{PacketTypeATCPosition, 7},
		{PacketTypeDeleteATC, 1},
		{PacketTypeDeletePilot, 1},
		{PacketTypeTextMessage, 3},
		{PacketTypeProController, 4},
		{PacketTypeSquawkbox, 3},
		{PacketTypeClientQuery, 3},
		{PacketTypeClientQueryResponse, 3},
		{PacketTypeMetarRequest, 4},
		{PacketTypeKillRequest, 3},
		{PacketTypeAuthChallenge, 3},
		{PacketTypeHandoffRequest, 3},
		{PacketTypeHandoffAccept, 3},
		{PacketTypeHandoffCancel, 3},
		{PacketTypeFlightPlan, 17},
		{PacketTypeFlightPlanAmendment, 18},
		{PacketTypeServerIdent, 4},
		{PacketTypeClientIdent, 8},
		{PacketTypeAddPilot, 8},
		{PacketTypeAddATC, 7},
		{PacketTypeError, 5},
		{PacketTypeUnknown, -1},
		{PacketType(999), -1},
	}
	for _, tt := range tests {
		got := MinFields(tt.t)
		if got != tt.want {
			t.Errorf("MinFields(%v) = %d, want %d", tt.t, got, tt.want)
		}
	}
}

func TestSourceCallsignAndVerify(t *testing.T) {
	// Pilot position: source is field 1
	pp := []byte("@S:GTI8197:2000:1:40.6:-73.7:26:0:1:0\r\n")
	if got := string(SourceCallsign(pp, PacketTypePilotPosition)); got != "GTI8197" {
		t.Errorf("pilot SourceCallsign = %q, want GTI8197", got)
	}
	if !VerifySourceCallsign(pp, PacketTypePilotPosition, "GTI8197") {
		t.Error("VerifySourceCallsign pilot should succeed")
	}
	if VerifySourceCallsign(pp, PacketTypePilotPosition, "OTHER") {
		t.Error("VerifySourceCallsign pilot should fail for OTHER")
	}

	// Text message: source is field 0 after prefix strip
	tm := []byte("#TMN7938C:@22800:hello\r\n")
	if got := string(SourceCallsign(tm, PacketTypeTextMessage)); got != "N7938C" {
		t.Errorf("tm SourceCallsign = %q, want N7938C", got)
	}
	if !VerifySourceCallsign(tm, PacketTypeTextMessage, "N7938C") {
		t.Error("VerifySourceCallsign tm should succeed")
	}

	// ATC position
	atc := []byte("%EWR_P_APP:28550:5:150:4:40.67:-74.18:0\r\n")
	if got := string(SourceCallsign(atc, PacketTypeATCPosition)); got != "EWR_P_APP" {
		t.Errorf("atc SourceCallsign = %q, want EWR_P_APP", got)
	}

	// Fast pilot
	fast := []byte("^DAL1151:40.6:-73.7:16:8:1:0:0:0:0:0:0:0\r\n")
	if got := string(SourceCallsign(fast, PacketTypePilotPositionFast)); got != "DAL1151" {
		t.Errorf("fast SourceCallsign = %q, want DAL1151", got)
	}
}
