package gui

import (
	"crypto/sha1"
	"encoding/hex"
)

func sha1SumHex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}
