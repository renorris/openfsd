package protocol

import "strings"

// ServerIdent is a $DI server identification packet.
type ServerIdent struct {
	Version      string
	ChallengeKey string
}

// Marshal encodes the packet as:
//
//	$DISERVER:CLIENT:{Version}:{ChallengeKey}\r\n
//
// openfsd sends: $DISERVER:CLIENT:openfsd:6f70656e667364\r\n
func (p ServerIdent) Marshal() []byte {
	var b strings.Builder
	b.Grow(32 + len(p.Version) + len(p.ChallengeKey))
	b.WriteString("$DISERVER:CLIENT:")
	b.WriteString(p.Version)
	b.WriteByte(':')
	b.WriteString(p.ChallengeKey)
	b.WriteString("\r\n")
	return []byte(b.String())
}

// ParseServerIdent parses a $DI line. Trailing \r\n is optional.
func ParseServerIdent(line []byte) (ServerIdent, error) {
	if TypeOf(line) != PacketTypeServerIdent {
		return ServerIdent{}, errPacket("server ident: wrong type")
	}
	if CountFields(line) < MinFields(PacketTypeServerIdent) {
		return ServerIdent{}, errPacket("server ident: too few fields")
	}
	return ServerIdent{
		Version:      string(Field(line, 2)),
		ChallengeKey: string(Field(line, 3)),
	}, nil
}
