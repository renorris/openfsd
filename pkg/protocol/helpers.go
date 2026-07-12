package protocol

// packetError is a parse/validation error for wire packets.
type packetError struct {
	msg string
}

func (e *packetError) Error() string { return e.msg }

func errPacket(msg string) error {
	return &packetError{msg: "protocol: " + msg}
}
