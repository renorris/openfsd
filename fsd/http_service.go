package fsd

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"
)

// runServiceHTTP starts the admin service HTTP server used for
// internal communication between the API HTTP server and this FSD server.
func (s *Server) runServiceHTTP(ctx context.Context) {
	e := s.setupRoutes()
	if err := e.Run(s.cfg.ServiceHTTPListenAddr); err != nil {
		slog.Error(err.Error())
	}
}

func (s *Server) setupRoutes() (e *gin.Engine) {
	e = gin.New()

	// Verify administrator service JWT
	e.Use(s.authMiddleware)
	e.GET("/online_users", s.handleGetOnlineUsers)
	e.POST("/kick_user", s.handleKickUser)

	return
}

func (s *Server) authMiddleware(c *gin.Context) {
	authHeader, found := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !found {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	jwtSecret, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		slog.Error(err.Error())
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	accessToken, err := ParseJwtToken(authHeader, []byte(jwtSecret))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	claims := accessToken.CustomClaims()
	if claims.TokenType != "fsd_service" || claims.NetworkRating < NetworkRatingAdministator {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	c.Next()
}

type OnlineUserGeneralData struct {
	Callsign         string    `json:"callsign"`
	CID              int       `json:"cid"`
	Name             string    `json:"name"`
	NetworkRating    int       `json:"network_rating"`
	MaxNetworkRating int       `json:"max_network_rating"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	LogonTime        time.Time `json:"logon_time"`
	LastUpdated      time.Time `json:"last_updated"`
}

type OnlineUserPilot struct {
	OnlineUserGeneralData
	Altitude    int    `json:"altitude"`
	Groundspeed int    `json:"groundspeed"`
	Heading     int    `json:"heading"`
	Transponder string `json:"transponder"`
}

type OnlineUserATC struct {
	OnlineUserGeneralData
	Frequency string `json:"frequency"`
	Facility  int    `json:"facility"`
	VisRange  int    `json:"visual_range"`
}

type OnlineUsersResponseData struct {
	Pilots []OnlineUserPilot `json:"pilots"`
	ATC    []OnlineUserATC   `json:"atc"`
}

func (s *Server) handleGetOnlineUsers(c *gin.Context) {
	s.postOffice.clientMapLock.RLock()
	mapLen := len(s.postOffice.clientMap)
	s.postOffice.clientMapLock.RUnlock()

	clientMap := make(map[string]*Client, mapLen+16)

	s.postOffice.clientMapLock.RLock()
	maps.Copy(clientMap, s.postOffice.clientMap)
	s.postOffice.clientMapLock.RUnlock()

	resData := OnlineUsersResponseData{
		Pilots: make([]OnlineUserPilot, 0, 512),
		ATC:    make([]OnlineUserATC, 0, 128),
	}

	for _, client := range clientMap {
		latLon := client.latLon()
		genData := OnlineUserGeneralData{
			Callsign:         client.callsign,
			CID:              client.cid,
			Name:             client.realName,
			NetworkRating:    int(client.networkRating),
			MaxNetworkRating: int(client.maxNetworkRating),
			Latitude:         latLon[0],
			Longitude:        latLon[1],
			LogonTime:        client.loginTime,
			LastUpdated:      client.lastUpdated.Load(),
		}

		if client.isAtc {
			atc := OnlineUserATC{
				OnlineUserGeneralData: genData,
				Frequency:             client.frequency.Load(),
				Facility:              client.facilityType,
				VisRange:              int(client.visRange.Load() * 0.000539957), // Convert meters to nautical miles
			}
			resData.ATC = append(resData.ATC, atc)
		} else {
			pilot := OnlineUserPilot{
				OnlineUserGeneralData: genData,
				Altitude:              int(client.altitude.Load()),
				Groundspeed:           int(client.groundspeed.Load()),
				Heading:               int(client.heading.Load()),
				Transponder:           client.transponder.Load(),
			}
			resData.Pilots = append(resData.Pilots, pilot)
		}
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	json.NewEncoder(c.Writer).Encode(&resData)
}

func (s *Server) handleKickUser(c *gin.Context) {
	type RequestBody struct {
		Callsign string `json:"callsign" binding:"required"`
	}

	var reqBody RequestBody
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
	}

	client, err := s.postOffice.find(reqBody.Callsign)
	if err != nil {
		if !errors.Is(err, ErrCallsignDoesNotExist) {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Cancelling the context will cause the client's event loop to close
	client.cancelCtx()

	c.AbortWithStatus(http.StatusNoContent)
}
