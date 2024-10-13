package web

import (
	"bytes"
	"github.com/renorris/openfsd/servercontext"
	"io"
	"net/http"
	"strings"
)

const statusFormat = `; IMPORTANT NOTE: This file can change as data sources change. Please check at regular intervals.
120218:NOTCP
;
json3={OPENFSD_ADDRESS}/api/v1/data/openfsd-data.json
;
url1={OPENFSD_ADDRESS}/api/v1/data/servers.txt
;
servers.live={OPENFSD_ADDRESS}/api/v1/data/servers.txt
;
voice0=afv
;
; END`

var formattedStatusTxt string

func formatStatusTxt() string {
	openfsdAddress := ""
	if servercontext.Config().TLSEnabled {
		openfsdAddress += "https://"
	} else {
		openfsdAddress += "http://"
	}

	if servercontext.Config().HTTPDomainName != "" {
		openfsdAddress += servercontext.Config().HTTPDomainName
	} else {
		openfsdAddress += servercontext.Config().DomainName
	}

	// Format string with {OPENFSD_ADDRESS}
	formatted := strings.Replace(statusFormat, "{OPENFSD_ADDRESS}", openfsdAddress, -1)

	// Ensure all line feeds also have carriage returns
	formatted = strings.Replace(formatted, "\n", "\r\n", -1)

	return formatted
}

var formattedStatusTxtEtag string

func StatusTxtHandler(w http.ResponseWriter, r *http.Request) {
	if formattedStatusTxt == "" {
		formattedStatusTxt = formatStatusTxt()
	}

	if formattedStatusTxtEtag == "" {
		formattedStatusTxtEtag = getEtag(formattedStatusTxt)
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("ETag", formattedStatusTxtEtag)
	w.Header().Set("Cache-Control", "max-age=60")

	w.WriteHeader(200)

	io.Copy(w, bytes.NewReader([]byte(formattedStatusTxt)))
}
