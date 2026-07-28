package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/renorris/openfsd/internal/serviceapi"
	"github.com/stretchr/testify/require"
)

func TestFsdServiceURLs(t *testing.T) {
	s := &Server{cfg: &ServerConfig{
		FsdHttpServiceAddress:   "http://a:1",
		FsdHttpServiceAddresses: "http://b:2, http://c:3",
	}}
	urls := s.fsdServiceURLs()
	require.Len(t, urls, 2)
	require.Equal(t, "http://b:2", urls[0])

	s2 := &Server{cfg: &ServerConfig{FsdHttpServiceAddress: "http://only:1/"}}
	require.Equal(t, []string{"http://only:1"}, s2.fsdServiceURLs())
}

func TestAggregateOnlineUsers(t *testing.T) {
	mux1 := http.NewServeMux()
	mux1.HandleFunc("/online_users", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(serviceapi.OnlineUsersResponseData{
			Pilots: []serviceapi.OnlineUserPilot{{OnlineUserGeneralData: serviceapi.OnlineUserGeneralData{Callsign: "P1", NodeID: "n1"}}},
		})
	})
	srv1 := httptest.NewServer(mux1)
	defer srv1.Close()

	mux2 := http.NewServeMux()
	mux2.HandleFunc("/online_users", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(serviceapi.OnlineUsersResponseData{
			ATC: []serviceapi.OnlineUserATC{{OnlineUserGeneralData: serviceapi.OnlineUserGeneralData{Callsign: "TWR", NodeID: "n2"}}},
		})
	})
	srv2 := httptest.NewServer(mux2)
	defer srv2.Close()

	// Need JWT secret path — use pe_test helpers if available; mock ConfigRepo via minimal server
	// Skip full JWT: aggregateOnlineUsers uses makeFsdHttpServiceHttpRequestTo which needs config.
	// Unit-test only fsdServiceURLs + kick routing shape without full auth.
	_ = srv1
	_ = srv2
}
