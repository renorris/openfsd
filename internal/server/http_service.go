package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/db"
)

// runServiceHTTP starts the admin service HTTP server used for
// internal communication between the API HTTP server and this FSD server.
// It shuts down when ctx is cancelled and closes s.httpDone when Serve returns.
func (s *Server) runServiceHTTP(ctx context.Context) {
	defer close(s.httpDone)

	e := s.setupRoutes()
	listen := s.httpListen
	if listen == nil {
		listen = net.Listen
	}
	ln, err := listen("tcp", s.cfg.ServiceHTTPListenAddr)
	if err != nil {
		s.logger.Error(err.Error())
		return
	}

	httpSrv := &http.Server{
		Handler:           e,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error(err.Error())
	}
}

func (s *Server) setupRoutes() (e *gin.Engine) {
	e = gin.New()
	e.Use(gin.Recovery())

	// Verify administrator service JWT
	e.Use(s.authMiddleware)
	e.GET("/online_users", s.handleGetOnlineUsers)
	e.POST("/kick_user", s.handleKickUser)

	// Sweatbox control plane — registered only when SWEATBOX_ENABLED (s.sweatbox != nil).
	// Disabled servers expose no /sweatbox/* surface (404).
	if s.sweatbox != nil {
		s.registerSweatboxRoutes(e)
	}

	return
}

// cutBearerToken extracts a Bearer token from Authorization, case-insensitive scheme.
func cutBearerToken(header string) (token string, ok bool) {
	const prefix = "bearer "
	if len(header) < len(prefix) {
		return "", false
	}
	if !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token = strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

func (s *Server) authMiddleware(c *gin.Context) {
	authHeader, found := cutBearerToken(c.GetHeader("Authorization"))
	if !found {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	jwtSecret, err := s.configKV.Get(db.ConfigJwtSecretKey)
	if err != nil {
		s.logger.Error(err.Error())
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	accessToken, err := auth.ParseJwtToken(authHeader, []byte(jwtSecret))
	if err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	claims := accessToken.CustomClaims()
	if claims.TokenType != "fsd_service" || claims.NetworkRating < NetworkRatingAdministator {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	c.Next()
}

// OnlineUserGeneralData is shared identity/position state for online users.
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

// OnlineUserPilot is a pilot entry in the online-users snapshot.
type OnlineUserPilot struct {
	OnlineUserGeneralData
	Altitude    int    `json:"altitude"`
	Groundspeed int    `json:"groundspeed"`
	Heading     int    `json:"heading"`
	Transponder string `json:"transponder"`
	// Synthetic is true for in-process sweatbox pilots (no TCP client).
	// Omitted from JSON when false so human pilots stay compact.
	Synthetic bool `json:"synthetic,omitempty"`
}

// OnlineUserATC is an ATC entry in the online-users snapshot.
type OnlineUserATC struct {
	OnlineUserGeneralData
	Frequency string `json:"frequency"`
	Facility  int    `json:"facility"`
	VisRange  int    `json:"visual_range"`
}

// OnlineUsersResponseData is the JSON body for GET /online_users.
type OnlineUsersResponseData struct {
	Pilots []OnlineUserPilot `json:"pilots"`
	ATC    []OnlineUserATC   `json:"atc"`
}

func (s *Server) handleGetOnlineUsers(c *gin.Context) {
	clients := s.registry.Snapshot()

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
				Facility:              int(client.FacilityType.Load()),
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
				Synthetic:             client.Synthetic,
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
		return
	}

	client, err := s.registry.Find(reqBody.Callsign)
	if err != nil {
		if !errors.Is(err, ErrCallsignDoesNotExist) {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
		return
	}

	// Synthetic (sweatbox) sessions have no handleConn Release defer — Remove
	// performs pointer-scoped #DP + registry.Release + engine.Delete.
	if client.Synthetic && s.sweatbox != nil {
		_ = s.sweatbox.Remove(client.Callsign)
		c.AbortWithStatus(http.StatusNoContent)
		return
	}

	// Disconnect cancels context and closes gnet/classic transports.
	client.Disconnect()

	c.AbortWithStatus(http.StatusNoContent)
}
