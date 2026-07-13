package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/server"
)

// handleFrontendDashboard renders the dashboard with a server-side connection
// summary. Leaflet map polling is progressive enhancement only — if JS fails,
// the HTML table/counts remain usable.
func (s *Server) handleFrontendDashboard(c *gin.Context) {
	claims := getJwtContext(c)
	page := dashboardPage{
		basePage: basePage{
			User:      pageUserFromClaims(claims),
			CSRFToken: s.issueCSRFToken(c),
		},
	}

	online, err := s.fetchOnlineUsers()
	if err != nil {
		page.SummaryUnavailable = true
		page.SummaryError = "Connection summary temporarily unavailable"
	} else {
		page.SummaryAvailable = true
		page.PilotCount = len(online.Pilots)
		page.ATCCount = len(online.ATC)
		page.ConnectionCount = page.PilotCount + page.ATCCount
		page.Connections = make([]connectionRow, 0, page.ConnectionCount)
		for _, p := range online.Pilots {
			page.Connections = append(page.Connections, connectionRow{
				Callsign: p.Callsign,
				CID:      p.CID,
				Name:     p.Name,
				Kind:     "pilot",
				Detail:   fmt.Sprintf("%d ft · %d kts · hdg %d", p.Altitude, p.Groundspeed, p.Heading),
			})
		}
		for _, a := range online.ATC {
			page.Connections = append(page.Connections, connectionRow{
				Callsign: a.Callsign,
				CID:      a.CID,
				Name:     a.Name,
				Kind:     "atc",
				Detail:   a.Frequency,
			})
		}
	}

	s.writeTemplate(c, "dashboard", page)
}

// fetchOnlineUsers queries the FSD HTTP service for the live connection list.
// Used by the dashboard HTML summary and by the datafeed cache worker.
func (s *Server) fetchOnlineUsers() (*server.OnlineUsersResponseData, error) {
	client := http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()

	req, err := s.makeFsdHttpServiceHttpRequest(http.MethodGet, "/online_users", nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FSD HTTP service status %d", res.StatusCode)
	}

	var online server.OnlineUsersResponseData
	if err := json.NewDecoder(res.Body).Decode(&online); err != nil {
		return nil, err
	}
	return &online, nil
}
