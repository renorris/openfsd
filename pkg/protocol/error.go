package protocol

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// FormatError builds an FSD $ER packet matching openfsd wire format:
//
//	$ERserver:unknown:%d::%s\r\n
//
// Note: openfsd uses lowercase "server", unpadded error code, and empty
// causing-parameter field — not the docs form $ERSERVER:unknown:006::...
func FormatError(code ErrorCode, message string) string {
	var b strings.Builder
	b.Grow(32 + len(message))
	b.WriteString("$ERserver:unknown:")
	b.WriteString(strconv.Itoa(int(code)))
	b.WriteString("::")
	b.WriteString(message)
	b.WriteString("\r\n")
	return b.String()
}

// WriteError writes a formatted $ER packet to w.
func WriteError(w io.Writer, code ErrorCode, message string) error {
	if w == nil {
		return fmt.Errorf("protocol: nil writer")
	}
	_, err := io.WriteString(w, FormatError(code, message))
	return err
}
