package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/renorris/openfsd/servercontext"
	"io"
	"net/http"
	"strings"
)

// TXT

const serverlistTextFormat = `!GENERAL:
VERSION = 8
RELOAD = 2
UPDATE = 20220401021210
ATIS ALLOW MIN = 5
CONNECTED CLIENTS = 1
;
;
!SERVERS:
{SERVERS_LIST}
;
;   END`

var formattedServerListTxt string

func formatServerListTxt() string {

	var domainName string
	if servercontext.Config().FSDDomainName != "" {
		domainName = servercontext.Config().FSDDomainName
	} else {
		domainName = servercontext.Config().DomainName
	}

	serversList := fmt.Sprintf("OPENFSD:%s:Everywhere:OPENFSD:1:", domainName)
	return strings.Replace(serverlistTextFormat, "{SERVERS_LIST}", serversList, -1)
}

var formattedServerListTxtEtag string

// JSON

type serverListEntry struct {
	Ident                    string `json:"ident"`
	HostnameOrIP             string `json:"hostname_or_ip"`
	Location                 string `json:"location"`
	Name                     string `json:"name"`
	ClientsConnectionAllowed int    `json:"clients_connection_allowed"`
	ClientConnectionAllowed  bool   `json:"client_connections_allowed"`
	IsSweatbox               bool   `json:"is_sweatbox"`
}

var formattedServerListJson = ""

func formatServerListJson() (str string, err error) {
	var domainName string
	if servercontext.Config().FSDDomainName != "" {
		domainName = servercontext.Config().FSDDomainName
	} else {
		domainName = servercontext.Config().DomainName
	}

	serverList := []serverListEntry{{
		Ident:                    "OPENFSD",
		HostnameOrIP:             domainName,
		Location:                 "Everywhere",
		Name:                     "OPENFSD",
		ClientsConnectionAllowed: 1,
		ClientConnectionAllowed:  true,
		IsSweatbox:               false,
	}}

	var serverListBytes []byte
	if serverListBytes, err = json.Marshal(&serverList); err != nil {
		return
	}

	str = string(serverListBytes)
	return
}

var formattedServerListJSONEtag = ""

// ServerListJsonHandler handles json server list calls
func ServerListJsonHandler(w http.ResponseWriter, r *http.Request) {
	if formattedServerListJson == "" {
		var err error
		if formattedServerListJson, err = formatServerListJson(); err != nil {
			http.Error(w, "internal server error: error marshalling server list JSON", http.StatusInternalServerError)
			return
		}
	}

	if formattedServerListJSONEtag == "" {
		formattedServerListJSONEtag = getEtag(formattedServerListJson)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", formattedServerListJSONEtag)
	w.Header().Set("Cache-Control", "max-age=60")

	w.WriteHeader(200)

	io.Copy(w, bytes.NewReader([]byte(formattedServerListJson)))
}

// ServerListTxtHandler handles text/plain server list calls
func ServerListTxtHandler(w http.ResponseWriter, r *http.Request) {
	if formattedServerListTxt == "" {
		formattedServerListTxt = formatServerListTxt()
	}

	if formattedServerListTxtEtag == "" {
		formattedServerListTxtEtag = getEtag(formattedServerListTxt)
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("ETag", formattedServerListTxtEtag)
	w.Header().Set("Cache-Control", "max-age=60")

	w.WriteHeader(200)

	io.Copy(w, bytes.NewReader([]byte(formattedServerListTxt)))
}
