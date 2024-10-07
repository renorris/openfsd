package web

import (
	"bytes"
	_ "embed"
	"io"
	"net/http"
)

//go:embed static/favicon.ico
var favicon []byte

func FaviconHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	w.WriteHeader(200)

	io.Copy(w, bytes.NewReader(favicon))
}
