package web

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
)

func writeResponseError(w http.ResponseWriter, status int, resp any) {
	w.WriteHeader(status)
	if respBytes, err := json.Marshal(&resp); err == nil {
		io.Copy(w, bytes.NewReader(respBytes))
	}
}

func generateRandomPassword() (string, error) {
	randBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, randBytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(randBytes), nil
}

func getEtag(str string) string {
	sum := sha1.Sum([]byte(str))
	sumSlice := sum[:]
	return hex.EncodeToString(sumSlice)
}
