package web

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/serviceapi"
	"github.com/renorris/openfsd/pkg/protocol"
)

// Public JSON request bodies for sweatbox mutations (design §D).
// Airport/scenario are JSON on the public API; FSD receives text/plain of the text field.

type sweatboxAirportJSONRequest struct {
	Text    string `json:"text"`
	Replace bool   `json:"replace"`
}

type sweatboxScenarioJSONRequest struct {
	Text string `json:"text"`
}

type sweatboxDeleteAllJSONRequest struct {
	Confirm bool `json:"confirm"`
}

// requireSweatboxI1 returns claims when the actor is Instructor1+; otherwise writes 403 and ok=false.
func requireSweatboxI1(c *gin.Context) (claims *auth.CustomClaims, ok bool) {
	claims = getJwtContext(c)
	if claims == nil || claims.NetworkRating < protocol.NetworkRatingInstructor1 {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return nil, false
	}
	return claims, true
}

// writeSweatboxProxyErr maps transport / unexpected FSD statuses to enveloped 502.
func writeSweatboxProxyErr(c *gin.Context, msg string) {
	res := newAPIV1Failure(msg)
	writeAPIV1Response(c, http.StatusBadGateway, &res)
}

func writeSweatboxDisabled(c *gin.Context) {
	res := newAPIV1Failure("Sweatbox is not enabled on the FSD server")
	writeAPIV1Response(c, http.StatusNotFound, &res)
}

// handleAPISweatboxState GET /api/v1/sweatbox/state
//
// Authenticated read proxy of FSD service HTTP GET /sweatbox/state.
// Used by sweatbox.js progressive enhancement (1–2 s poll). Cookie session
// or Bearer; CSRF not required for GET. Min rating Instructor1+.
//
// On success (and FSD 404 disabled), the FSD JSON body is passed through so
// the PE client can use the same shape as the service control plane.
func (s *Server) handleAPISweatboxState(c *gin.Context) {
	if _, ok := requireSweatboxI1(c); !ok {
		return
	}

	status, body, err := s.fsdSweatboxDo(http.MethodGet, "/sweatbox/state", "", nil)
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}

	switch status {
	case http.StatusOK, http.StatusNotFound:
		// 404 = sweatbox disabled on FSD (no /sweatbox/* routes).
		c.Data(status, "application/json", body)
	case http.StatusUnauthorized, http.StatusForbidden:
		writeSweatboxProxyErr(c, "FSD service rejected the request")
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxOps GET /api/v1/sweatbox/ops
//
// Read proxy of FSD GET /sweatbox/ops (elapsed / arr / dep / ops-per-min).
// Optional for PE; state already includes most of these fields.
// Min rating Instructor1+.
func (s *Server) handleAPISweatboxOps(c *gin.Context) {
	if _, ok := requireSweatboxI1(c); !ok {
		return
	}

	status, body, err := s.fsdSweatboxDo(http.MethodGet, "/sweatbox/ops", "", nil)
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}

	switch status {
	case http.StatusOK, http.StatusNotFound:
		c.Data(status, "application/json", body)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxSession GET /api/v1/sweatbox/session
//
// Enveloped parallel of GET /state for third-party / operator clients.
// data = serviceapi.SweatboxStateJSON. Raw /state remains for PE.
func (s *Server) handleAPISweatboxSession(c *gin.Context) {
	if _, ok := requireSweatboxI1(c); !ok {
		return
	}

	status, body, err := s.fsdSweatboxDo(http.MethodGet, "/sweatbox/state", "", nil)
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusOK:
		var st serviceapi.SweatboxStateJSON
		if err := json.Unmarshal(body, &st); err != nil {
			writeSweatboxProxyErr(c, "FSD service returned unexpected status")
			return
		}
		if st.Aircraft == nil {
			st.Aircraft = []serviceapi.SweatboxAircraftJSON{}
		}
		res := newAPIV1Success(&st)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusNotFound:
		writeSweatboxDisabled(c)
	case http.StatusUnauthorized, http.StatusForbidden:
		writeSweatboxProxyErr(c, "FSD service rejected the request")
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxAirport POST /api/v1/sweatbox/airport
//
// JSON {"text","replace"} → FSD POST /sweatbox/airport[?replace=1] text/plain.
// Status mapping per design §D.
func (s *Server) handleAPISweatboxAirport(c *gin.Context) {
	claims, ok := requireSweatboxI1(c)
	if !ok {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebMaxBody)
	var req sweatboxAirportJSONRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if isRequestTooLarge(err) {
			res := newAPIV1Failure("Airport payload too large (max 2 MiB)")
			writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
			return
		}
		res := newAPIV1Failure("invalid JSON body")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	text := req.Text
	if len(bytes.TrimSpace([]byte(text))) == 0 {
		res := newAPIV1Failure("Airport file or text is required")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if len(text) > sweatboxWebMaxBody {
		res := newAPIV1Failure("Airport payload too large (max 2 MiB)")
		writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
		return
	}

	path := "/sweatbox/airport"
	if req.Replace {
		path += "?replace=1"
	}

	status, respBody, err := s.fsdSweatboxDo(http.MethodPost, path, "text/plain; charset=utf-8", strings.NewReader(text))
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}

	switch status {
	case http.StatusOK:
		var data serviceapi.SweatboxAirportLoadResponse
		if err := json.Unmarshal(respBody, &data); err != nil {
			writeSweatboxProxyErr(c, "FSD service returned unexpected status")
			return
		}
		if data.Errors == nil {
			data.Errors = []string{}
		}
		slog.Info("sweatbox api airport load",
			"cid", claims.CID,
			"icao", data.ICAO,
			"surfaces", data.Surfaces,
			"replace", req.Replace,
			"content_length", len(text),
		)
		res := newAPIV1Success(&data)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusBadRequest:
		res := newAPIV1Failure(firstJSONError(respBody, "Invalid airport file"))
		writeAPIV1Response(c, http.StatusBadRequest, &res)
	case http.StatusConflict:
		res := newAPIV1Failure(firstJSONError(respBody, "Aircraft present — check Replace to clear them, or delete aircraft first"))
		writeAPIV1Response(c, http.StatusConflict, &res)
	case http.StatusNotFound:
		writeSweatboxDisabled(c)
	case http.StatusRequestEntityTooLarge:
		res := newAPIV1Failure("Airport payload too large (max 2 MiB)")
		writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxScenario POST /api/v1/sweatbox/scenario
//
// JSON {"text"} → FSD POST /sweatbox/scenario text/plain.
func (s *Server) handleAPISweatboxScenario(c *gin.Context) {
	claims, ok := requireSweatboxI1(c)
	if !ok {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebMaxBody)
	var req sweatboxScenarioJSONRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if isRequestTooLarge(err) {
			res := newAPIV1Failure("Scenario payload too large (max 2 MiB)")
			writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
			return
		}
		res := newAPIV1Failure("invalid JSON body")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	text := req.Text
	if len(bytes.TrimSpace([]byte(text))) == 0 {
		res := newAPIV1Failure("Scenario file or text is required")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	if len(text) > sweatboxWebMaxBody {
		res := newAPIV1Failure("Scenario payload too large (max 2 MiB)")
		writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
		return
	}

	status, respBody, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/scenario", "text/plain; charset=utf-8", strings.NewReader(text))
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}

	switch status {
	case http.StatusOK:
		var data serviceapi.SweatboxScenarioResponse
		if err := json.Unmarshal(respBody, &data); err != nil {
			writeSweatboxProxyErr(c, "FSD service returned unexpected status")
			return
		}
		if data.Errors == nil {
			data.Errors = []string{}
		}
		slog.Info("sweatbox api scenario load",
			"cid", claims.CID,
			"loaded", data.Loaded,
			"error_count", len(data.Errors),
			"content_length", len(text),
		)
		res := newAPIV1Success(&data)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusBadRequest:
		res := newAPIV1Failure(firstJSONError(respBody, "Invalid scenario file"))
		writeAPIV1Response(c, http.StatusBadRequest, &res)
	case http.StatusConflict:
		res := newAPIV1Failure(firstJSONError(respBody, "No airport loaded — load a .apt first"))
		writeAPIV1Response(c, http.StatusConflict, &res)
	case http.StatusNotFound:
		writeSweatboxDisabled(c)
	case http.StatusRequestEntityTooLarge:
		res := newAPIV1Failure("Scenario payload too large (max 2 MiB)")
		writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxCommand POST /api/v1/sweatbox/command
//
// JSON SweatboxCommandRequest → FSD. Soft-fail (ok:false) stays HTTP 200 + envelope data.
func (s *Server) handleAPISweatboxCommand(c *gin.Context) {
	claims, ok := requireSweatboxI1(c)
	if !ok {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebSmallFormMaxBody)
	var req serviceapi.SweatboxCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if isRequestTooLarge(err) {
			res := newAPIV1Failure("Request body too large")
			writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
			return
		}
		res := newAPIV1Failure("invalid JSON body")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}
	req.Command = strings.TrimSpace(req.Command)
	req.Callsign = strings.TrimSpace(req.Callsign)
	if req.Command == "" {
		res := newAPIV1Failure("Command is required")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	payload, err := json.Marshal(req)
	if err != nil {
		res := newAPIV1Failure("Unable to encode command")
		writeAPIV1Response(c, http.StatusInternalServerError, &res)
		return
	}

	status, respBody, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/command", "application/json", bytes.NewReader(payload))
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}

	switch status {
	case http.StatusOK:
		var data serviceapi.SweatboxCommandResponse
		if err := json.Unmarshal(respBody, &data); err != nil {
			writeSweatboxProxyErr(c, "FSD service returned unexpected status")
			return
		}
		slog.Info("sweatbox api command",
			"cid", claims.CID,
			"ok", data.OK,
			"callsign", req.Callsign,
		)
		// Soft-fail (ok:false) stays 200 with data — do not elevate to 4xx.
		res := newAPIV1Success(&data)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusBadRequest:
		res := newAPIV1Failure("Invalid command request")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
	case http.StatusConflict:
		res := newAPIV1Failure(firstJSONError(respBody, "No airport loaded"))
		writeAPIV1Response(c, http.StatusConflict, &res)
	case http.StatusNotFound:
		writeSweatboxDisabled(c)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxPause POST /api/v1/sweatbox/pause — FSD 204/200 → public 200 envelope.
func (s *Server) handleAPISweatboxPause(c *gin.Context) {
	if _, ok := requireSweatboxI1(c); !ok {
		return
	}

	status, _, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/pause", "", nil)
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		res := newAPIV1Success(nil)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusNotFound:
		writeSweatboxDisabled(c)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxUnpause POST /api/v1/sweatbox/unpause — FSD 204/200 → public 200 envelope.
func (s *Server) handleAPISweatboxUnpause(c *gin.Context) {
	if _, ok := requireSweatboxI1(c); !ok {
		return
	}

	status, _, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/unpause", "", nil)
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		res := newAPIV1Success(nil)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusNotFound:
		writeSweatboxDisabled(c)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxDeleteAircraft DELETE /api/v1/sweatbox/aircraft/:callsign
func (s *Server) handleAPISweatboxDeleteAircraft(c *gin.Context) {
	if _, ok := requireSweatboxI1(c); !ok {
		return
	}

	cs := strings.TrimSpace(c.Param("callsign"))
	if cs == "" {
		res := newAPIV1Failure("Callsign is required")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	path := "/sweatbox/aircraft/" + url.PathEscape(cs)
	status, _, err := s.fsdSweatboxDo(http.MethodDelete, path, "", nil)
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		res := newAPIV1Success(nil)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusNotFound:
		// Unknown callsign or sweatbox disabled — same HTTP 404 from FSD.
		res := newAPIV1Failure("Aircraft not found (or sweatbox disabled): " + cs)
		writeAPIV1Response(c, http.StatusNotFound, &res)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}

// handleAPISweatboxDeleteAllAircraft DELETE /api/v1/sweatbox/aircraft
//
// Requires confirm=true (JSON body or query confirm=1).
func (s *Server) handleAPISweatboxDeleteAllAircraft(c *gin.Context) {
	if _, ok := requireSweatboxI1(c); !ok {
		return
	}

	confirmed := queryTruthy(c.Query("confirm"))
	if !confirmed {
		// Optional JSON body {"confirm":true}; empty body is fine when query used.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebSmallFormMaxBody)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			if isRequestTooLarge(err) {
				res := newAPIV1Failure("Request body too large")
				writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
				return
			}
			res := newAPIV1Failure("invalid JSON body")
			writeAPIV1Response(c, http.StatusBadRequest, &res)
			return
		}
		if len(bytes.TrimSpace(body)) > 0 {
			var req sweatboxDeleteAllJSONRequest
			if err := json.Unmarshal(body, &req); err != nil {
				res := newAPIV1Failure("invalid JSON body")
				writeAPIV1Response(c, http.StatusBadRequest, &res)
				return
			}
			confirmed = req.Confirm
		}
	}
	if !confirmed {
		res := newAPIV1Failure("confirm required")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	status, _, err := s.fsdSweatboxDo(http.MethodDelete, "/sweatbox/aircraft", "", nil)
	if err != nil {
		writeSweatboxProxyErr(c, "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		res := newAPIV1Success(nil)
		writeAPIV1Response(c, http.StatusOK, &res)
	case http.StatusNotFound:
		writeSweatboxDisabled(c)
	default:
		writeSweatboxProxyErr(c, "FSD service returned unexpected status")
	}
}
