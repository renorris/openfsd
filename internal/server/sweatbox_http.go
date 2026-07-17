package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/sweatbox"
)

// Max body size for sweatbox airport/scenario/command payloads (design: 2 MiB).
const sweatboxMaxBodyBytes = 2 << 20

// SweatboxCommandRequest is the JSON body for POST /sweatbox/command.
type SweatboxCommandRequest struct {
	// Callsign is optional UI-selected aircraft for aircraft-scoped verbs.
	Callsign string `json:"callsign"`
	// Command is the instructor text command (required).
	Command string `json:"command"`
}

// SweatboxCommandResponse is returned for POST /sweatbox/command.
// Soft validation failures use HTTP 200 with OK=false (TWRTrainer-style).
type SweatboxCommandResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// SweatboxScenarioResponse is returned for POST /sweatbox/scenario.
type SweatboxScenarioResponse struct {
	Loaded int      `json:"loaded"`
	Errors []string `json:"errors"`
}

// SweatboxOpsJSON is returned for GET /sweatbox/ops.
type SweatboxOpsJSON struct {
	ElapsedSec float64 `json:"elapsed_sec"`
	ArrCount   int     `json:"arr_count"`
	DepCount   int     `json:"dep_count"`
	OpsPerMin  float64 `json:"ops_per_min"`
	Message    string  `json:"message,omitempty"`
}

// registerSweatboxRoutes mounts /sweatbox/* when the host is allocated.
// Call only when s.sweatbox != nil (gated by SWEATBOX_ENABLED).
func (s *Server) registerSweatboxRoutes(e *gin.Engine) {
	g := e.Group("/sweatbox")
	g.GET("/state", s.handleSweatboxState)
	g.GET("/ops", s.handleSweatboxOps)
	g.GET("/stats", s.handleSweatboxOps) // alias
	g.POST("/airport", s.handleSweatboxAirport)
	g.POST("/scenario", s.handleSweatboxScenario)
	g.POST("/command", s.handleSweatboxCommand)
	g.POST("/pause", s.handleSweatboxPause)
	g.POST("/unpause", s.handleSweatboxUnpause)
	g.DELETE("/aircraft/:callsign", s.handleSweatboxDeleteAircraft)
	g.DELETE("/aircraft", s.handleSweatboxDeleteAllAircraft)
	// Convenience POST del (mirrors instructor "del" without command shell).
	g.POST("/aircraft/:callsign/del", s.handleSweatboxDeleteAircraft)
	g.POST("/del", s.handleSweatboxDeleteAircraftBody)
}

func (s *Server) handleSweatboxState(c *gin.Context) {
	st := s.sweatbox.State()
	if st.Aircraft == nil {
		st.Aircraft = []SweatboxAircraftJSON{}
	}
	writeJSON(c, http.StatusOK, st)
}

func (s *Server) handleSweatboxOps(c *gin.Context) {
	ops := s.sweatbox.Ops()
	writeJSON(c, http.StatusOK, SweatboxOpsJSON{
		ElapsedSec: ops.Elapsed.Seconds(),
		ArrCount:   ops.ArrCount,
		DepCount:   ops.DepCount,
		OpsPerMin:  ops.OpsPerMin,
		Message:    formatSweatboxOpsMessage(ops),
	})
}

// formatSweatboxOpsMessage mirrors sweatbox.formatOpsMessage (unexported) for the
// JSON snapshot field. ApplyCommand("ops") remains available via /command.
func formatSweatboxOpsMessage(ops sweatbox.OpsStats) string {
	sec := int(ops.Elapsed.Seconds())
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	return fmt.Sprintf("Elapsed: %d:%02d:%02d  Arrivals: %d  Departures: %d  Ops/min: %.2f",
		h, m, s, ops.ArrCount, ops.DepCount, ops.OpsPerMin)
}

func (s *Server) handleSweatboxAirport(c *gin.Context) {
	body, err := readSweatboxBody(c)
	if err != nil {
		abortBodyError(c, err)
		return
	}
	replace := queryTruthy(c.Query("replace"))
	res, err := s.sweatbox.LoadAirportResult(body, replace)
	if err != nil {
		if errors.Is(err, errSweatboxDisabled) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		// Aircraft present without replace, or other engine rejection.
		msg := err.Error()
		if strings.Contains(msg, "aircraft are present") {
			writeJSON(c, http.StatusConflict, gin.H{
				"icao":     res.ICAO,
				"surfaces": res.Surfaces,
				"errors":   append([]string{msg}, res.Errors...),
			})
			return
		}
		if res.ICAO == "" || strings.Contains(msg, "invalid airport") {
			writeJSON(c, http.StatusBadRequest, gin.H{
				"icao":     res.ICAO,
				"surfaces": res.Surfaces,
				"errors":   nonEmptyErrors(res.Errors, msg),
			})
			return
		}
		writeJSON(c, http.StatusBadRequest, gin.H{
			"icao":     res.ICAO,
			"surfaces": res.Surfaces,
			"errors":   nonEmptyErrors(res.Errors, msg),
		})
		return
	}
	if res.Errors == nil {
		res.Errors = []string{}
	}
	writeJSON(c, http.StatusOK, res)
}

func (s *Server) handleSweatboxScenario(c *gin.Context) {
	body, err := readSweatboxBody(c)
	if err != nil {
		abortBodyError(c, err)
		return
	}
	// No airport → 409 (design).
	if s.sweatbox.Engine() == nil || s.sweatbox.Engine().Airport() == nil {
		writeJSON(c, http.StatusConflict, SweatboxScenarioResponse{
			Loaded: 0,
			Errors: []string{"No airport loaded."},
		})
		return
	}
	loaded, errs := s.sweatbox.LoadScenario(body)
	out := SweatboxScenarioResponse{
		Loaded: loaded,
		Errors: make([]string, 0, len(errs)),
	}
	for _, e := range errs {
		if e != nil {
			out.Errors = append(out.Errors, e.Error())
		}
	}
	// Host maps "No airport" into errs when race; treat pure no-airport as 409.
	if loaded == 0 && len(out.Errors) == 1 && strings.Contains(out.Errors[0], "No airport") {
		writeJSON(c, http.StatusConflict, out)
		return
	}
	writeJSON(c, http.StatusOK, out)
}

func (s *Server) handleSweatboxCommand(c *gin.Context) {
	// Enforce body size even for JSON commands.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxMaxBodyBytes)

	var req SweatboxCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || isMaxBytes(err) {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	// Soft "no airport" for add-style commands → 409 when engine has no airport.
	result := s.sweatbox.ApplyCommandSelected(req.Callsign, cmd)
	if !result.OK && strings.Contains(result.Message, "No airport") {
		writeJSON(c, http.StatusConflict, SweatboxCommandResponse{
			OK:      false,
			Message: result.Message,
		})
		return
	}
	writeJSON(c, http.StatusOK, SweatboxCommandResponse{
		OK:      result.OK,
		Message: result.Message,
	})
}

func (s *Server) handleSweatboxPause(c *gin.Context) {
	s.sweatbox.Pause()
	c.Status(http.StatusNoContent)
}

func (s *Server) handleSweatboxUnpause(c *gin.Context) {
	s.sweatbox.Unpause()
	c.Status(http.StatusNoContent)
}

func (s *Server) handleSweatboxDeleteAircraft(c *gin.Context) {
	cs := c.Param("callsign")
	if cs == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if err := s.sweatbox.Remove(cs); err != nil {
		if errors.Is(err, errSweatboxNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleSweatboxDeleteAircraftBody(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxMaxBodyBytes)
	var req struct {
		Callsign string `json:"callsign"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Callsign) == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if err := s.sweatbox.Remove(req.Callsign); err != nil {
		if errors.Is(err, errSweatboxNotFound) {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleSweatboxDeleteAllAircraft(c *gin.Context) {
	s.sweatbox.RemoveAll()
	c.Status(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Body helpers
// ---------------------------------------------------------------------------

func readSweatboxBody(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxMaxBodyBytes)

	ct := c.ContentType()
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(sweatboxMaxBodyBytes); err != nil {
			return nil, err
		}
		if fh, err := c.FormFile("file"); err == nil && fh != nil {
			f, err := fh.Open()
			if err != nil {
				return nil, err
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, sweatboxMaxBodyBytes+1))
			if err != nil {
				return nil, err
			}
			if len(data) > sweatboxMaxBodyBytes {
				return nil, &http.MaxBytesError{Limit: sweatboxMaxBodyBytes}
			}
			return data, nil
		}
		for _, key := range []string{"file", "airport", "scenario", "body"} {
			if v := c.PostForm(key); v != "" {
				if len(v) > sweatboxMaxBodyBytes {
					return nil, &http.MaxBytesError{Limit: sweatboxMaxBodyBytes}
				}
				return []byte(v), nil
			}
		}
		return nil, errors.New("multipart body missing file or text field")
	}

	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	if len(data) > sweatboxMaxBodyBytes {
		return nil, &http.MaxBytesError{Limit: sweatboxMaxBodyBytes}
	}
	return data, nil
}

func abortBodyError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if isMaxBytes(err) {
		c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		return
	}
	c.AbortWithStatus(http.StatusBadRequest)
}

func isMaxBytes(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	// Older / wrapped messages from MaxBytesReader.
	msg := err.Error()
	return strings.Contains(msg, "http: request body too large") ||
		strings.Contains(msg, "request body too large")
}

func queryTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func nonEmptyErrors(errs []string, fallback string) []string {
	if len(errs) > 0 {
		return errs
	}
	if fallback != "" {
		return []string{fallback}
	}
	return []string{}
}

func writeJSON(c *gin.Context, status int, v any) {
	c.Header("Content-Type", "application/json")
	c.Status(status)
	_ = json.NewEncoder(c.Writer).Encode(v)
}
