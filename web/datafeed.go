package web

import (
	"bytes"
	"github.com/renorris/openfsd/servercontext"
	"io"
	"net/http"
)

func DataFeedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	feed, lastModified := servercontext.DataFeed().Feed()
	w.Header().Set("Last-Modified", lastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", "public, max-age=15")

	w.WriteHeader(200)

	io.Copy(w, bytes.NewReader([]byte(feed)))
}
