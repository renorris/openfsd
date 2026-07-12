package protocol

import "bytes"

// CountFields returns the number of colon-delimited fields in packet.
// An empty packet is treated as having one (empty) field.
func CountFields(packet []byte) int {
	return bytes.Count(packet, []byte(":")) + 1
}

// rebaseToNextField advances packet past the next ':' delimiter.
// If no delimiter exists, IndexByte returns -1 and the result is packet[0:],
// matching historical fsd/util.go behavior.
func rebaseToNextField(packet []byte) []byte {
	return packet[bytes.IndexByte(packet, ':')+1:]
}

// Field returns the field at index (0-based). Trailing "\r\n" is stripped.
// Negative indices yield nil (protocol package never panics).
// Out-of-range positive indices follow historical fsd getField behavior:
// once past the last delimiter, the last remaining slice is returned repeatedly.
func Field(packet []byte, index int) []byte {
	if index < 0 {
		return nil
	}

	for range index {
		packet = rebaseToNextField(packet)
	}

	if i := bytes.IndexByte(packet, ':'); i != -1 {
		packet = packet[:i]
	}

	packet, _ = bytes.CutSuffix(packet, []byte("\r\n"))
	return packet
}
