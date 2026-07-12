package protocol

import "strings"

// packetError is a parse/validation error for wire packets.
type packetError struct {
	msg string
}

func (e *packetError) Error() string { return e.msg }

func errPacket(msg string) error {
	return &packetError{msg: "protocol: " + msg}
}

// cutPrefixField strips prefix from a field value.
// Returns the remainder and whether the prefix was present.
func cutPrefixField(field []byte, prefix string) (string, bool) {
	s := string(field)
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return s[len(prefix):], true
}
