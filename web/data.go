package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/fsd"
	"go.uber.org/atomic"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"text/template"
	"time"
)

//go:embed data_templates/status.txt
var statusTxtRawTemplate string
var statusTxtTemplate *template.Template

//go:embed data_templates/servers.txt
var serversTxtRawTemplate string
var serversTxtTemplate *template.Template

func init() {
	var err error
	statusTxtTemplate = template.New("statustxt")
	if statusTxtTemplate, err = statusTxtTemplate.Parse(statusTxtRawTemplate); err != nil {
		panic("Unable to parse status.txt template: " + err.Error())
	}

	serversTxtTemplate = template.New("serverstxt")
	if serversTxtTemplate, err = serversTxtTemplate.Parse(serversTxtRawTemplate); err != nil {
		panic("Unable to parse servers.txt template: " + err.Error())
	}
}

func (s *Server) handleGetStatusTxt(c *gin.Context) {
	baseURL, ok := s.getBaseURLOrErr(c)
	if !ok {
		return
	}

	// Generate a new status.txt
	statusTxt, err := generateStatusTxt(baseURL)
	if err != nil {
		c.Writer.WriteHeader(http.StatusInternalServerError)
		c.Writer.WriteString("Error generating status.txt")
		slog.Error(err.Error())
		return
	}

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteString(statusTxt)
}

func generateStatusTxt(baseURL string) (txt string, err error) {
	type TemplateData struct {
		ApiServerBaseURL string
	}

	tmplData := TemplateData{ApiServerBaseURL: baseURL}

	buf := bytes.Buffer{}
	buf.Grow(1024)
	if err = statusTxtTemplate.Execute(&buf, &tmplData); err != nil {
		return
	}

	// Ensure all newlines have a carriage return
	txt = strings.ReplaceAll(buf.String(), "\n", "\r\n")
	return
}

type DataJsonStatus struct {
	Data map[string][]string `json:"data"`
}

func (s *Server) handleGetStatusJSON(c *gin.Context) {
	baseURL, ok := s.getBaseURLOrErr(c)
	if !ok {
		return
	}

	statusJson := DataJsonStatus{
		Data: map[string][]string{
			"v3": {
				baseURL + "/api/v1/data/openfsd-data.json",
			},
			"servers": {
				baseURL + "/api/v1/data/openfsd-servers.json",
			},
			"servers_sweatbox": {
				baseURL + "/api/v1/data/sweatbox-servers.json",
			},
			"servers_all": {
				baseURL + "/api/v1/data/all-servers.json",
			},
		},
	}

	res, err := json.Marshal(&statusJson)
	if err != nil {
		slog.Error(err.Error())
		writePlaintext500Error(c, "Unable to marshal JSON")
		return
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Write(res)
}

type DataJsonServer struct {
	Ident                    string `json:"ident"`
	HostnameOrIp             string `json:"hostname_or_ip"`
	Location                 string `json:"location"`
	Name                     string `json:"name"`
	ClientsConnectionAllowed int    `json:"clients_connection_allowed"`
	ClientConnectionsAllowed bool   `json:"client_connections_allowed"`
	IsSweatbox               bool   `json:"is_sweatbox"`
}

func (s *Server) handleGetServersJSON(c *gin.Context) {
	serverIdent, serverHostname, serverLocation, err := s.getFsdServerInfo()
	if err != nil {
		writePlaintext500Error(c, "Unable to load FSD server info from configuration")
		return
	}

	_, isSweatbox := c.Get("is_sweatbox")

	type ServersJson []DataJsonServer
	dataJson := ServersJson{
		{
			Ident:                    serverIdent,
			HostnameOrIp:             serverHostname,
			Location:                 serverLocation,
			Name:                     serverIdent,
			ClientConnectionsAllowed: true,
			ClientsConnectionAllowed: 99,
			IsSweatbox:               isSweatbox,
		},
		{
			Ident:                    "AUTOMATIC",
			HostnameOrIp:             serverHostname,
			Location:                 serverLocation,
			Name:                     serverIdent,
			ClientConnectionsAllowed: true,
			ClientsConnectionAllowed: 99,
			IsSweatbox:               isSweatbox,
		},
	}

	res, err := json.Marshal(&dataJson)
	if err != nil {
		slog.Error(err.Error())
		writePlaintext500Error(c, "Unable to marshal JSON")
		return
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Write(res)
}

func (s *Server) handleGetServersTxt(c *gin.Context) {
	serversTxt, err := s.generateServersTxt()
	if err != nil {
		c.Writer.WriteHeader(http.StatusInternalServerError)
		c.Writer.WriteString("Error generating status.txt")
		slog.Error(err.Error())
		return
	}

	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteString(serversTxt)
}

func (s *Server) generateServersTxt() (txt string, err error) {
	serverIdent, serverHostname, serverLocation, err := s.getFsdServerInfo()
	if err != nil {
		slog.Error(err.Error())
		return
	}

	type TemplateData []DataJsonServer
	tmplData := TemplateData{
		{
			Ident:                    serverIdent,
			HostnameOrIp:             serverHostname,
			Location:                 serverLocation,
			Name:                     serverIdent,
			ClientConnectionsAllowed: true,
			ClientsConnectionAllowed: 99,
			IsSweatbox:               false,
		},
		{
			Ident:                    "AUTOMATIC",
			HostnameOrIp:             serverHostname,
			Location:                 serverLocation,
			Name:                     serverIdent,
			ClientConnectionsAllowed: true,
			ClientsConnectionAllowed: 99,
			IsSweatbox:               false,
		},
	}

	buf := bytes.Buffer{}
	buf.Grow(1024)
	if err = serversTxtTemplate.Execute(&buf, &tmplData); err != nil {
		return
	}

	// Ensure all newlines have a carriage return
	txt = strings.ReplaceAll(buf.String(), "\n", "\r\n")
	return
}

func (s *Server) getFsdServerInfo() (serverIdent string, serverHostname string, serverLocation string, err error) {
	serverIdent, err = s.dbRepo.ConfigRepo.Get(db.ConfigFsdServerIdent)
	if err != nil {
		slog.Error(err.Error())
		return
	}

	serverHostname, err = s.dbRepo.ConfigRepo.Get(db.ConfigFsdServerHostname)
	if err != nil {
		slog.Error(err.Error())
		return
	}

	serverLocation, err = s.dbRepo.ConfigRepo.Get(db.ConfigFsdServerLocation)
	if err != nil {
		slog.Error(err.Error())
		return
	}

	return
}

func writePlaintext500Error(c *gin.Context, msg string) {
	c.Writer.Header().Set("Content-Type", "text/plain")
	c.Writer.WriteHeader(http.StatusInternalServerError)
	c.Writer.WriteString(msg)
}

func (s *Server) getBaseURLOrErr(c *gin.Context) (baseURL string, ok bool) {
	baseURL, err := s.dbRepo.ConfigRepo.Get(db.ConfigApiServerBaseURL)
	if err != nil {
		c.Writer.WriteHeader(http.StatusInternalServerError)
		if !errors.Is(err, db.ErrConfigKeyNotFound) {
			slog.Error(err.Error())
			return
		}
		errMsg := "API server base URL is not set in the config"
		slog.Error(errMsg)
		c.Writer.WriteString(errMsg)
		return
	}

	ok = true
	return
}

type Datafeed struct {
	Pilots []fsd.OnlineUserPilot `json:"pilots"`
	ATC    []fsd.OnlineUserATC   `json:"atc"`
}

type DatafeedCache struct {
	jsonStr     string
	lastUpdated time.Time
}

var datafeedCache atomic.Pointer[DatafeedCache]

func (s *Server) getDatafeed(c *gin.Context) {
	feed := datafeedCache.Load()
	if feed == nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.WriteString(feed.jsonStr)
}

func (s *Server) generateDatafeed() (feed *DatafeedCache, err error) {
	client := http.Client{}
	req, err := s.makeFsdHttpServiceHttpRequest("GET", "/online_users", nil)
	if err != nil {
		return
	}
	res, err := client.Do(req)
	if err != nil {
		return
	}

	if res.StatusCode != http.StatusOK {
		err = errors.New("FSD HTTP service returned a non-200 status code")
		return
	}

	decoder := json.NewDecoder(res.Body)
	onlineUsers := fsd.OnlineUsersResponseData{}
	if err = decoder.Decode(&onlineUsers); err != nil {
		return
	}

	dataFeed := Datafeed{
		Pilots: onlineUsers.Pilots,
		ATC:    onlineUsers.ATC,
	}
	buf := bytes.Buffer{}
	encoder := json.NewEncoder(&buf)
	if err = encoder.Encode(&dataFeed); err != nil {
		return
	}

	feed = &DatafeedCache{
		jsonStr:     buf.String(),
		lastUpdated: time.Now(),
	}
	return
}

// makeFsdHttpServiceHttpRequest prepares an HTTP request destined for the internal FSD HTTP API.
//
// method sets the HTTP method, path is the relative HTTP path (e.g. /online_users), and body is an optional request body.
func (s *Server) makeFsdHttpServiceHttpRequest(method string, path string, body io.Reader) (req *http.Request, err error) {
	// Generate JWT bearer token
	customFields := fsd.CustomFields{
		TokenType:     "fsd_service",
		CID:           -1,
		NetworkRating: fsd.NetworkRatingAdministator,
	}
	token, err := fsd.MakeJwtToken(&customFields, 15*time.Minute)
	if err != nil {
		return
	}
	secretKey, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		return
	}
	tokenStr, err := token.SignedString([]byte(secretKey))
	if err != nil {
		return
	}

	url := s.cfg.FsdHttpServiceAddress + path
	req, err = http.NewRequest(method, url, body)
	if err != nil {
		return
	}

	req.Header.Set("Authorization", "Bearer "+tokenStr)

	return
}

func (s *Server) runDatafeedWorker(ctx context.Context) {
	s.updateDataFeedCache()
	ticker := time.NewTicker(15 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.updateDataFeedCache()
		}
	}
}

func (s *Server) updateDataFeedCache() {
	feed, err := s.generateDatafeed()
	if err != nil {
		slog.Error(err.Error())
		return
	}
	datafeedCache.Store(feed)
}
