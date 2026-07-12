package protocol

import "bytes"

// OpaquePacket is a lightly parsed packet retaining the raw wire bytes.
// Source/Dest are best-effort extractions for common layouts.
type OpaquePacket struct {
	Type   PacketType
	Raw    []byte
	Source string
	Dest   string
}

// ParseOpaque classifies line and extracts source/dest when present.
func ParseOpaque(line []byte) (OpaquePacket, error) {
	// Keep a copy so callers may retain Raw without aliasing scanner buffers.
	raw := append([]byte(nil), line...)
	t := TypeOf(raw)
	if t == PacketTypeUnknown {
		return OpaquePacket{Type: t, Raw: raw}, errPacket("opaque: unknown packet type")
	}

	p := OpaquePacket{
		Type: t,
		Raw:  raw,
	}

	switch t {
	case PacketTypePilotPosition:
		// @MODE:CALLSIGN:... — no destination field
		p.Source = string(SourceCallsign(raw, t))
	case PacketTypeATCPosition, PacketTypePilotPositionFast,
		PacketTypePilotPositionSlow, PacketTypePilotPositionStopped:
		// prefix+CALLSIGN:rest — no destination
		p.Source = string(SourceCallsign(raw, t))
	case PacketTypeServerIdent, PacketTypeError:
		p.Source = string(SourceCallsign(raw, t))
		if CountFields(raw) >= 2 {
			p.Dest = string(Field(raw, 1))
		}
	default:
		// Most other types: PREFIX+SOURCE:DEST:...
		p.Source = string(SourceCallsign(raw, t))
		if CountFields(raw) >= 2 {
			p.Dest = string(Field(raw, 1))
		}
	}

	// Strip trailing CR/LF from Dest/Source for consistency with Field.
	p.Source = string(bytes.TrimSuffix([]byte(p.Source), []byte("\r\n")))
	p.Dest = string(bytes.TrimSuffix([]byte(p.Dest), []byte("\r\n")))
	return p, nil
}
