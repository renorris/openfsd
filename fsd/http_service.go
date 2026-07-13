package fsd

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/postoffice"
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

	accessToken, err := auth.ParseJwtToken(authHeader, []byte(jwtSecret))
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
	clients := s.postOffice.Snapshot()

	resData := OnlineUsersResponseData{
		Pilots: make([]OnlineUserPilot, 0, 512),
		ATC:    make([]OnlineUserATC, 0, 128),
	}

	for _, client := range clients {
		latLon := client.LatLon()
		genData := OnlineUserGeneralData{
			Callsign:         client.Callsign,
			CID:              client.CID,
			Name:             client.RealName,
			NetworkRating:    int(client.NetworkRating),
			MaxNetworkRating: int(client.MaxNetworkRating),
			Latitude:         latLon[0],
			Longitude:        latLon[1],
			LogonTime:        client.LoginTime,
			LastUpdated:      client.LastUpdated.Load(),
		}

		if client.IsAtc {
			atc := OnlineUserATC{
				OnlineUserGeneralData: genData,
				Frequency:             client.Frequency.Load(),
				Facility:              client.FacilityType,
				VisRange:              int(client.VisRange.Load() * 0.000539957), // Convert meters to nautical miles
			}
			resData.ATC = append(resData.ATC, atc)
		} else {
			pilot := OnlineUserPilot{
				OnlineUserGeneralData: genData,
				Altitude:              int(client.Altitude.Load()),
				Groundspeed:           int(client.Groundspeed.Load()),
				Heading:               int(client.Heading.Load()),
				Transponder:           client.Transponder.Load(),
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

	client, err := s.postOffice.Find(reqBody.Callsign)
	if err != nil {
		if !errors.Is(err, postoffice.ErrCallsignDoesNotExist) {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Cancelling the context will cause the client's event loop to close
	client.Cancel()

	c.AbortWithStatus(http.StatusNoContent)
}
