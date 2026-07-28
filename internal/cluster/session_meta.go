package cluster

// SessionMeta is the HomeRPC QuerySessionMeta payload (R2-6).
// Wire: u16 fplLen | fpl | u16 bcLen | beacon | u8 isATC | u32be rating
type SessionMeta struct {
	FPLInfo string
	Beacon  string
	IsATC   bool
	Rating  int
}

// EncodeSessionMeta serializes SessionMeta.
func EncodeSessionMeta(m SessionMeta) []byte {
	fpl := SanitizeMeshString(m.FPLInfo, 2048)
	bc := SanitizeMeshString(m.Beacon, 8)
	var b []byte
	b = encodeString(b, fpl)
	b = encodeString(b, bc)
	if m.IsATC {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	b = encodeU32(b, uint32(m.Rating))
	return b
}

// DecodeSessionMeta parses SessionMeta.
func DecodeSessionMeta(p []byte) (SessionMeta, error) {
	var m SessionMeta
	var err error
	m.FPLInfo, p, err = decodeString(p)
	if err != nil {
		return m, err
	}
	m.Beacon, p, err = decodeString(p)
	if err != nil {
		return m, err
	}
	if len(p) < 1 {
		return m, ErrShortFrame
	}
	m.IsATC = p[0] != 0
	p = p[1:]
	var r uint32
	r, _, err = decodeU32(p)
	if err != nil {
		return m, err
	}
	m.Rating = int(r)
	return m, nil
}
