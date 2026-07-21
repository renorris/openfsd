package protocol

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "packets", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	// Fixtures are stored without trailing CRLF; wire form has \r\n.
	line := bytes.TrimRight(b, "\r\n")
	return append(line, '\r', '\n')
}

func TestServerIdentGolden(t *testing.T) {
	openfsd := loadFixture(t, "server_ident_openfsd.txt")
	want := []byte("$DISERVER:CLIENT:openfsd:6f70656e667364\r\n")
	if !bytes.Equal(openfsd, want) {
		t.Fatalf("fixture mismatch: got %q", openfsd)
	}

	p := ServerIdent{Version: "openfsd", ChallengeKey: "6f70656e667364"}
	got := p.Marshal()
	if !bytes.Equal(got, want) {
		t.Errorf("Marshal = %q, want %q", got, want)
	}

	parsed, err := ParseServerIdent(openfsd)
	if err != nil {
		t.Fatalf("ParseServerIdent: %v", err)
	}
	if parsed.Version != "openfsd" || parsed.ChallengeKey != "6f70656e667364" {
		t.Errorf("parsed = %+v", parsed)
	}

	// VATSIM docs example
	vatsim := loadFixture(t, "server_ident_vatsim.txt")
	parsed, err = ParseServerIdent(vatsim)
	if err != nil {
		t.Fatalf("ParseServerIdent vatsim: %v", err)
	}
	if parsed.Version != "VATSIM FSD V3.50" || parsed.ChallengeKey != "76617473696d" {
		t.Errorf("vatsim parsed = %+v", parsed)
	}

	// error paths
	if _, err := ParseServerIdent([]byte("@S:x")); err == nil {
		t.Error("expected wrong type error")
	}
	if _, err := ParseServerIdent([]byte("$DISERVER:CLIENT:only")); err == nil {
		t.Error("expected too few fields error")
	}
}

func TestClientIdentGolden(t *testing.T) {
	line := loadFixture(t, "client_ident.txt")
	p, err := ParseClientIdent(line)
	if err != nil {
		t.Fatalf("ParseClientIdent: %v", err)
	}
	if p.Callsign != "N172SP" || p.To != "SERVER" || p.SoftwareID != "88e4" ||
		p.SoftwareName != "vPilot" || p.VersionMajor != 3 || p.VersionMinor != 8 ||
		p.CID != 123456 || p.SystemUID != -582057156 || p.ChallengeKey != "6d6973746176" ||
		!p.HasChallengeKey {
		t.Errorf("parsed = %+v", p)
	}

	// Round-trip
	marshaled := p.Marshal()
	// Field values should match (float/int formatting may differ only in exact string for numbers we control)
	p2, err := ParseClientIdent(marshaled)
	if err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}
	if p2.Callsign != p.Callsign || p2.CID != p.CID || p2.ChallengeKey != p.ChallengeKey {
		t.Errorf("round-trip mismatch: %+v vs %+v", p, p2)
	}

	// Without challenge (8 fields)
	noChal := []byte("$IDN172SP:SERVER:88e4:vPilot:3:8:123456:-582057156\r\n")
	p3, err := ParseClientIdent(noChal)
	if err != nil {
		t.Fatalf("no challenge: %v", err)
	}
	if p3.HasChallengeKey || p3.ChallengeKey != "" {
		t.Errorf("expected no challenge, got %+v", p3)
	}
	// Marshal without challenge when empty
	out := ClientIdent{
		Callsign: "N172SP", To: "SERVER", SoftwareID: "88e4", SoftwareName: "vPilot",
		VersionMajor: 3, VersionMinor: 8, CID: 123456, SystemUID: -1,
	}.Marshal()
	if strings.Count(string(out), ":") != 7 {
		t.Errorf("expected 8 fields (7 colons), got %q", out)
	}
	// default To
	out = ClientIdent{Callsign: "X", SoftwareID: "1", SoftwareName: "n", VersionMajor: 1, VersionMinor: 0, CID: 1, SystemUID: 1}.Marshal()
	if !bytes.Contains(out, []byte(":SERVER:")) {
		t.Errorf("default To not applied: %q", out)
	}

	// error paths
	if _, err := ParseClientIdent([]byte("@S:x")); err == nil {
		t.Error("wrong type")
	}
	if _, err := ParseClientIdent([]byte("$IDx:SERVER:1:n:a:b:c")); err == nil {
		t.Error("too few fields")
	}
	if _, err := ParseClientIdent([]byte("$IDx:SERVER:1:n:bad:0:1:2")); err == nil {
		t.Error("bad major")
	}
	if _, err := ParseClientIdent([]byte("$IDx:SERVER:1:n:1:bad:1:2")); err == nil {
		t.Error("bad minor")
	}
	if _, err := ParseClientIdent([]byte("$IDx:SERVER:1:n:1:0:bad:2")); err == nil {
		t.Error("bad cid")
	}
	if _, err := ParseClientIdent([]byte("$IDx:SERVER:1:n:1:0:1:bad")); err == nil {
		t.Error("bad uid")
	}
}

func TestAddPilotGolden(t *testing.T) {
	line := loadFixture(t, "add_pilot.txt")
	p, err := ParseAddPilot(line)
	if err != nil {
		t.Fatalf("ParseAddPilot: %v", err)
	}
	if p.Callsign != "N7938C" || p.To != "SERVER" || p.CID != "100000" ||
		p.NetworkRating != NetworkRatingObserver || p.ProtoRevision != 101 ||
		p.SimulatorType != 2 || p.RealName != "John Doe" {
		t.Errorf("parsed = %+v", p)
	}
	if !strings.HasPrefix(p.Token, "eyJ") {
		t.Errorf("token missing: %q", p.Token)
	}

	// Round-trip structural equality
	p2, err := ParseAddPilot(p.Marshal())
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if p2.Callsign != p.Callsign || p2.CID != p.CID || p2.RealName != p.RealName ||
		p2.NetworkRating != p.NetworkRating || p2.ProtoRevision != p.ProtoRevision {
		t.Errorf("round-trip %+v vs %+v", p, p2)
	}

	// default To
	out := AddPilot{Callsign: "X", CID: "1", Token: "t", NetworkRating: 1, ProtoRevision: 100, SimulatorType: 0, RealName: "N"}.Marshal()
	if !bytes.Contains(out, []byte(":SERVER:")) {
		t.Errorf("default To: %q", out)
	}

	// errors
	if _, err := ParseAddPilot([]byte("#AAX:SERVER:N:1:t:1:100")); err == nil {
		t.Error("wrong type")
	}
	if _, err := ParseAddPilot([]byte("#APX:SERVER:1:t:1:100:0")); err == nil {
		t.Error("too few")
	}
	if _, err := ParseAddPilot([]byte("#APX:SERVER:1:t:bad:100:0:N")); err == nil {
		t.Error("bad rating")
	}
	if _, err := ParseAddPilot([]byte("#APX:SERVER:1:t:1:bad:0:N")); err == nil {
		t.Error("bad proto")
	}
	if _, err := ParseAddPilot([]byte("#APX:SERVER:1:t:1:100:bad:N")); err == nil {
		t.Error("bad sim")
	}
}

func TestAddATCGolden(t *testing.T) {
	line := loadFixture(t, "add_atc.txt")
	p, err := ParseAddATC(line)
	if err != nil {
		t.Fatalf("ParseAddATC: %v", err)
	}
	if p.Callsign != "RN_OBS" || p.RealName != "John Doe" || p.CID != "100000" ||
		p.NetworkRating != NetworkRatingStudent1 || p.ProtoRevision != 100 {
		t.Errorf("parsed = %+v", p)
	}

	p2, err := ParseAddATC(p.Marshal())
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if p2.Callsign != p.Callsign || p2.RealName != p.RealName || p2.ProtoRevision != p.ProtoRevision {
		t.Errorf("round-trip %+v vs %+v", p, p2)
	}

	out := AddATC{Callsign: "X", RealName: "N", CID: "1", Token: "t", NetworkRating: 2, ProtoRevision: 100}.Marshal()
	if !bytes.Contains(out, []byte(":SERVER:")) {
		t.Errorf("default To: %q", out)
	}

	if _, err := ParseAddATC([]byte("#APX:SERVER:1:t:1:100:0:N")); err == nil {
		t.Error("wrong type")
	}
	if _, err := ParseAddATC([]byte("#AAX:SERVER:N:1:t:1")); err == nil {
		t.Error("too few")
	}
	if _, err := ParseAddATC([]byte("#AAX:SERVER:N:1:t:bad:100")); err == nil {
		t.Error("bad rating")
	}
	if _, err := ParseAddATC([]byte("#AAX:SERVER:N:1:t:1:bad")); err == nil {
		t.Error("bad proto")
	}
}

func TestPilotPositionGolden(t *testing.T) {
	line := loadFixture(t, "pilot_position.txt")
	p, err := ParsePilotPosition(line)
	if err != nil {
		t.Fatalf("ParsePilotPosition: %v", err)
	}
	if p.TransponderMode != "S" || p.Callsign != "GTI8197" || p.TransponderCode != "2000" ||
		p.NetworkRating != NetworkRatingObserver || p.Latitude != 40.65906 || p.Longitude != -73.79891 ||
		p.TrueAltitude != 26 || p.Groundspeed != 0 || p.PitchBankHeading != 4290776072 ||
		p.AltitudeCorrection != 359 {
		t.Errorf("parsed = %+v", p)
	}

	// Round-trip parse of marshaled
	p2, err := ParsePilotPosition(p.Marshal())
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if p2.Callsign != p.Callsign || p2.PitchBankHeading != p.PitchBankHeading || p2.AltitudeCorrection != p.AltitudeCorrection {
		t.Errorf("round-trip %+v vs %+v", p, p2)
	}

	// 9-field form (no altitude correction)
	short := []byte("@S:GTI8197:2000:1:40.65906:-73.79891:26:0:4290776072\r\n")
	p3, err := ParsePilotPosition(short)
	if err != nil {
		t.Fatalf("9-field: %v", err)
	}
	if p3.AltitudeCorrection != 0 {
		t.Errorf("expected zero corr, got %d", p3.AltitudeCorrection)
	}

	// errors
	if _, err := ParsePilotPosition([]byte("%X:1:2:3:4:5:6:7")); err == nil {
		t.Error("wrong type")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:1:1:1:1:1")); err == nil {
		t.Error("too few")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:bad:1:1:1:1:1")); err == nil {
		t.Error("bad rating")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:1:bad:1:1:1:1")); err == nil {
		t.Error("bad lat")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:1:1:bad:1:1:1")); err == nil {
		t.Error("bad lon")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:1:1:1:bad:1:1")); err == nil {
		t.Error("bad alt")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:1:1:1:1:bad:1")); err == nil {
		t.Error("bad gs")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:1:1:1:1:1:bad")); err == nil {
		t.Error("bad pbh")
	}
	if _, err := ParsePilotPosition([]byte("@S:X:1:1:1:1:1:1:1:bad")); err == nil {
		t.Error("bad corr")
	}
}

func TestATCPositionGolden(t *testing.T) {
	line := loadFixture(t, "atc_position.txt")
	p, err := ParseATCPosition(line)
	if err != nil {
		t.Fatalf("ParseATCPosition: %v", err)
	}
	if p.Callsign != "EWR_P_APP" || p.Frequencies != "28550" || p.FacilityType != 5 ||
		p.VisibilityRange != 150 || p.NetworkRating != NetworkRatingStudent3 ||
		p.Latitude != 40.67317 || p.Longitude != -74.18533 || p.UnknownZero != 0 {
		t.Errorf("parsed = %+v", p)
	}

	p2, err := ParseATCPosition(p.Marshal())
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if p2.Callsign != p.Callsign || p2.FacilityType != p.FacilityType {
		t.Errorf("round-trip %+v vs %+v", p, p2)
	}

	// 7-field without trailing zero
	short := []byte("%EWR_P_APP:28550:5:150:4:40.67317:-74.18533\r\n")
	p3, err := ParseATCPosition(short)
	if err != nil {
		t.Fatalf("7-field: %v", err)
	}
	if p3.UnknownZero != 0 {
		t.Errorf("expected 0, got %d", p3.UnknownZero)
	}

	if _, err := ParseATCPosition([]byte("@S:X:1:1:1:1:1:1:1")); err == nil {
		t.Error("wrong type")
	}
	if _, err := ParseATCPosition([]byte("%X:1:2:3:4:5")); err == nil {
		t.Error("too few")
	}
	if _, err := ParseATCPosition([]byte("%X:1:bad:3:4:5:6")); err == nil {
		t.Error("bad fac")
	}
	if _, err := ParseATCPosition([]byte("%X:1:2:bad:4:5:6")); err == nil {
		t.Error("bad vis")
	}
	if _, err := ParseATCPosition([]byte("%X:1:2:3:bad:5:6")); err == nil {
		t.Error("bad rating")
	}
	if _, err := ParseATCPosition([]byte("%X:1:2:3:4:bad:6")); err == nil {
		t.Error("bad lat")
	}
	if _, err := ParseATCPosition([]byte("%X:1:2:3:4:5:bad")); err == nil {
		t.Error("bad lon")
	}
	if _, err := ParseATCPosition([]byte("%X:1:2:3:4:5:6:bad")); err == nil {
		t.Error("bad zero")
	}
}

func TestSecondaryVisCenterGolden(t *testing.T) {
	// Wire confirmed from vPilot decompile (RossCarlson bm / PDUSecondaryVisCenter):
	// 'CALLSIGN:INDEX:LAT:LON with #0.00000 lat/lon.
	line := loadFixture(t, "secondary_vis_center.txt")
	p, err := ParseSecondaryVisCenter(line)
	if err != nil {
		t.Fatalf("ParseSecondaryVisCenter: %v", err)
	}
	if p.Callsign != "LAX_CTR" || p.Index != 0 ||
		p.Latitude != 34.05200 || p.Longitude != -118.24300 {
		t.Errorf("parsed = %+v", p)
	}

	got := p.Marshal()
	want := []byte("'LAX_CTR:0:34.05200:-118.24300\r\n")
	if !bytes.Equal(got, want) {
		t.Errorf("Marshal = %q, want %q", got, want)
	}

	p2, err := ParseSecondaryVisCenter(got)
	if err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if p2 != p {
		t.Errorf("round-trip %+v vs %+v", p, p2)
	}

	if _, err := ParseSecondaryVisCenter([]byte("%X:0:1:2\r\n")); err == nil {
		t.Error("wrong type")
	}
	if _, err := ParseSecondaryVisCenter([]byte("'X:0:1\r\n")); err == nil {
		t.Error("too few")
	}
	if _, err := ParseSecondaryVisCenter([]byte("'X:bad:1:2\r\n")); err == nil {
		t.Error("bad index")
	}
	if _, err := ParseSecondaryVisCenter([]byte("'X:0:bad:2\r\n")); err == nil {
		t.Error("bad lat")
	}
	if _, err := ParseSecondaryVisCenter([]byte("'X:0:1:bad\r\n")); err == nil {
		t.Error("bad lon")
	}

	op, err := ParseOpaque(line)
	if err != nil {
		t.Fatalf("ParseOpaque: %v", err)
	}
	if op.Type != PacketTypeSecondaryVisCenter || op.Source != "LAX_CTR" || op.Dest != "" {
		t.Errorf("opaque = %+v", op)
	}
}

func TestOpaquePacket(t *testing.T) {
	tm := loadFixture(t, "text_message.txt")
	op, err := ParseOpaque(tm)
	if err != nil {
		t.Fatalf("ParseOpaque tm: %v", err)
	}
	if op.Type != PacketTypeTextMessage || op.Source != "N7938C" || op.Dest != "@22800" {
		t.Errorf("opaque tm = %+v", op)
	}

	pp := loadFixture(t, "pilot_position.txt")
	op, err = ParseOpaque(pp)
	if err != nil {
		t.Fatalf("ParseOpaque pp: %v", err)
	}
	if op.Type != PacketTypePilotPosition || op.Source != "GTI8197" || op.Dest != "" {
		t.Errorf("opaque pp = %+v", op)
	}

	atc := loadFixture(t, "atc_position.txt")
	op, err = ParseOpaque(atc)
	if err != nil {
		t.Fatalf("ParseOpaque atc: %v", err)
	}
	if op.Source != "EWR_P_APP" || op.Dest != "" {
		t.Errorf("opaque atc = %+v", op)
	}

	si := loadFixture(t, "server_ident_openfsd.txt")
	op, err = ParseOpaque(si)
	if err != nil {
		t.Fatalf("ParseOpaque si: %v", err)
	}
	if op.Type != PacketTypeServerIdent || op.Source != "SERVER" || op.Dest != "CLIENT" {
		t.Errorf("opaque si = %+v", op)
	}

	er := loadFixture(t, "error_openfsd.txt")
	op, err = ParseOpaque(er)
	if err != nil {
		t.Fatalf("ParseOpaque er: %v", err)
	}
	if op.Type != PacketTypeError || op.Source != "server" || op.Dest != "unknown" {
		t.Errorf("opaque er = %+v", op)
	}

	fast := loadFixture(t, "fast_pilot_position.txt")
	op, err = ParseOpaque(fast)
	if err != nil {
		t.Fatalf("ParseOpaque fast: %v", err)
	}
	if op.Type != PacketTypePilotPositionFast || op.Source != "DAL1151" {
		t.Errorf("opaque fast = %+v", op)
	}

	slow := loadFixture(t, "pilot_position_slow.txt")
	op, err = ParseOpaque(slow)
	if err != nil {
		t.Fatalf("ParseOpaque slow: %v", err)
	}
	if op.Type != PacketTypePilotPositionSlow || op.Source != "PRM4211" {
		t.Errorf("opaque slow = %+v", op)
	}

	stopped := loadFixture(t, "pilot_position_stopped.txt")
	op, err = ParseOpaque(stopped)
	if err != nil {
		t.Fatalf("ParseOpaque stopped: %v", err)
	}
	if op.Type != PacketTypePilotPositionStopped || op.Source != "DAL2119" {
		t.Errorf("opaque stopped = %+v", op)
	}

	if _, err := ParseOpaque([]byte("garbage")); err == nil {
		t.Error("expected unknown type error")
	}
}

func TestErrorGoldenMatchesFormatError(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "packets", "error_openfsd.txt"))
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.TrimRight(raw, "\r\n")
	got := strings.TrimSuffix(FormatError(InvalidLogonError, "Invalid CID/password"), "\r\n")
	if string(raw) != got {
		t.Errorf("fixture %q != FormatError %q", raw, got)
	}
}

func TestNetworkRatingAndErrorCodeValues(t *testing.T) {
	if NetworkRatingInactive != -1 {
		t.Fatalf("Inactive = %d", NetworkRatingInactive)
	}
	if NetworkRatingObserver != 1 {
		t.Fatalf("Observer = %d", NetworkRatingObserver)
	}
	if NetworkRatingAdministator != 12 {
		t.Fatalf("Administator = %d", NetworkRatingAdministator)
	}
	if CallsignInUseError != 1 || ClientAuthenticationResponseTimeoutError != 17 {
		t.Fatalf("error code range")
	}
	if SyntaxError != 4 || InvalidLogonError != 6 {
		t.Fatalf("error codes")
	}
}

func TestPacketErrorString(t *testing.T) {
	err := errPacket("test msg")
	if err.Error() != "protocol: test msg" {
		t.Errorf("Error() = %q", err.Error())
	}
}
