package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/server"
)

// Max body for airport/scenario form payloads (mirrors FSD service HTTP 2 MiB).
const sweatboxWebMaxBody = 2 << 20

// Max body for small sweatbox form POSTs (command/pause/delete — not file uploads).
const sweatboxWebSmallFormMaxBody = 64 << 10

// Max freeform flash message length embedded in redirect Location query.
const sweatboxFlashMsgMaxRunes = 240

// handleFrontendSweatbox GET /sweatbox — server-rendered instructor page.
// Works with JS disabled: aircraft table from FSD GET /sweatbox/state + forms.
func (s *Server) handleFrontendSweatbox(c *gin.Context) {
	page := s.newSweatboxPage(c)
	s.applySweatboxFlash(c, &page)
	s.populateSweatboxState(c, &page)
	s.writeTemplate(c, "sweatbox", page)
}

func (s *Server) newSweatboxPage(c *gin.Context) sweatboxPage {
	claims := getJwtContext(c)
	return sweatboxPage{
		basePage: basePage{
			User:      pageUserFromClaims(claims),
			CSRFToken: s.issueCSRFToken(c),
		},
		Aircraft: []sweatboxAircraftRow{},
	}
}

func (s *Server) applySweatboxFlash(c *gin.Context, page *sweatboxPage) {
	msg := truncateRunes(strings.TrimSpace(c.Query("msg")), sweatboxFlashMsgMaxRunes)
	switch c.Query("flash") {
	case "ok":
		if msg == "" {
			msg = "Done"
		}
		page.FlashSuccess = msg
	case "err":
		if msg == "" {
			msg = "Request failed"
		}
		page.FlashError = msg
	case "airport_ok":
		if msg == "" {
			msg = "Airport loaded"
		}
		page.FlashSuccess = msg
	case "scenario_ok":
		if msg == "" {
			msg = "Scenario loaded"
		}
		page.FlashSuccess = msg
	case "paused":
		page.FlashSuccess = "Simulation paused"
	case "unpaused":
		page.FlashSuccess = "Simulation unpaused"
	case "deleted":
		if msg == "" {
			msg = "Aircraft deleted"
		}
		page.FlashSuccess = msg
	case "deleted_all":
		page.FlashSuccess = "All sweatbox aircraft deleted"
	}
}

func (s *Server) populateSweatboxState(c *gin.Context, page *sweatboxPage) {
	status, body, err := s.fsdSweatboxDo(http.MethodGet, "/sweatbox/state", "", nil)
	if err != nil {
		page.Unavailable = true
		page.StatusMsg = "Sweatbox control plane unavailable (FSD HTTP service unreachable). Ensure the FSD process is running and SWEATBOX_ENABLED is set."
		slog.Debug("sweatbox state fetch failed", "err", err.Error())
		return
	}
	switch status {
	case http.StatusOK:
		// continue decode below
	case http.StatusNotFound:
		page.Disabled = true
		page.StatusMsg = "Sweatbox is not enabled on the FSD server. Set SWEATBOX_ENABLED=true and restart openfsd."
		return
	case http.StatusUnauthorized, http.StatusForbidden:
		page.Unavailable = true
		page.StatusMsg = "Sweatbox control plane rejected the service request (auth)."
		return
	default:
		page.Unavailable = true
		page.StatusMsg = fmt.Sprintf("Sweatbox control plane returned HTTP %d", status)
		return
	}

	var st server.SweatboxStateJSON
	if err := json.Unmarshal(body, &st); err != nil {
		page.Unavailable = true
		page.StatusMsg = "Unable to parse sweatbox state from FSD"
		slog.Error("sweatbox state decode", "err", err.Error())
		return
	}

	page.Available = true
	page.ICAO = st.ICAO
	page.Paused = st.Paused
	page.Elapsed = formatSweatboxElapsed(st.Elapsed)
	page.ArrCount = st.ArrCount
	page.DepCount = st.DepCount
	page.Aircraft = make([]sweatboxAircraftRow, 0, len(st.Aircraft))
	for _, ac := range st.Aircraft {
		page.Aircraft = append(page.Aircraft, sweatboxAircraftRow{
			Callsign:    ac.Callsign,
			Type:        ac.Type,
			Rules:       ac.Rules,
			Squawk:      ac.Squawk,
			Heading:     formatSweatboxInt(ac.Heading),
			Alt:         formatSweatboxInt(ac.Alt),
			Speed:       formatSweatboxInt(ac.Speed),
			Status:      ac.Status,
			Instruction: ac.Instruction,
		})
	}
	if page.ICAO == "" {
		page.StatusMsg = "No airport loaded. Upload or paste a .apt file to begin."
	}
}

// ---------------------------------------------------------------------------
// Form mutations (CSRF + PRG)
// ---------------------------------------------------------------------------

// handleFrontendSweatboxAirport POST /sweatbox/airport
func (s *Server) handleFrontendSweatboxAirport(c *gin.Context) {
	// Cap before CSRF form parse so oversized bodies fail closed early.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebMaxBody+4096)
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	body, err := readSweatboxFormPayload(c, "file", "airport", "apt_text")
	if err != nil {
		if isRequestTooLarge(err) {
			s.redirectSweatboxFlash(c, "err", "Airport payload too large (max 2 MiB)")
			return
		}
		s.redirectSweatboxFlash(c, "err", "Airport file or text is required")
		return
	}
	if len(bytes.TrimSpace(body)) == 0 {
		s.redirectSweatboxFlash(c, "err", "Airport file or text is required")
		return
	}

	path := "/sweatbox/airport"
	if queryTruthy(c.PostForm("replace")) {
		path += "?replace=1"
	}

	status, respBody, err := s.fsdSweatboxDo(http.MethodPost, path, "text/plain; charset=utf-8", bytes.NewReader(body))
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusOK:
		icao := decodeSweatboxJSONField(respBody, "icao")
		msg := "Airport loaded"
		if icao != "" {
			msg = "Airport loaded: " + icao
		}
		s.redirectSweatboxFlash(c, "airport_ok", msg)
	case http.StatusNotFound:
		s.redirectSweatboxFlash(c, "err", "Sweatbox is not enabled on the FSD server")
	case http.StatusConflict:
		s.redirectSweatboxFlash(c, "err", firstJSONError(respBody, "Aircraft present — check Replace to clear them, or delete aircraft first"))
	case http.StatusBadRequest:
		s.redirectSweatboxFlash(c, "err", firstJSONError(respBody, "Invalid airport file"))
	case http.StatusRequestEntityTooLarge:
		s.redirectSweatboxFlash(c, "err", "Airport payload too large (max 2 MiB)")
	default:
		s.redirectSweatboxFlash(c, "err", fmt.Sprintf("Airport load failed (HTTP %d)", status))
	}
}

// handleFrontendSweatboxScenario POST /sweatbox/scenario
func (s *Server) handleFrontendSweatboxScenario(c *gin.Context) {
	// Cap before CSRF form parse so oversized bodies fail closed early.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebMaxBody+4096)
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	body, err := readSweatboxFormPayload(c, "file", "scenario", "air_text")
	if err != nil {
		if isRequestTooLarge(err) {
			s.redirectSweatboxFlash(c, "err", "Scenario payload too large (max 2 MiB)")
			return
		}
		s.redirectSweatboxFlash(c, "err", "Scenario file or text is required")
		return
	}
	if len(bytes.TrimSpace(body)) == 0 {
		s.redirectSweatboxFlash(c, "err", "Scenario file or text is required")
		return
	}

	status, respBody, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/scenario", "text/plain; charset=utf-8", bytes.NewReader(body))
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusOK:
		var res server.SweatboxScenarioResponse
		if err := json.Unmarshal(respBody, &res); err != nil {
			s.redirectSweatboxFlash(c, "err", "Unable to parse scenario response")
			return
		}
		msg := fmt.Sprintf("Scenario loaded: %d aircraft", res.Loaded)
		if len(res.Errors) > 0 {
			msg = fmt.Sprintf("%s (%d warning(s))", msg, len(res.Errors))
		}
		s.redirectSweatboxFlash(c, "scenario_ok", msg)
	case http.StatusNotFound:
		s.redirectSweatboxFlash(c, "err", "Sweatbox is not enabled on the FSD server")
	case http.StatusConflict:
		s.redirectSweatboxFlash(c, "err", firstJSONError(respBody, "No airport loaded — load a .apt first"))
	case http.StatusBadRequest:
		s.redirectSweatboxFlash(c, "err", firstJSONError(respBody, "Invalid scenario file"))
	case http.StatusRequestEntityTooLarge:
		s.redirectSweatboxFlash(c, "err", "Scenario payload too large (max 2 MiB)")
	default:
		s.redirectSweatboxFlash(c, "err", fmt.Sprintf("Scenario load failed (HTTP %d)", status))
	}
}

// handleFrontendSweatboxCommand POST /sweatbox/command
func (s *Server) handleFrontendSweatboxCommand(c *gin.Context) {
	if !s.limitSweatboxSmallForm(c) {
		return
	}
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	cmd := strings.TrimSpace(c.PostForm("command"))
	callsign := strings.TrimSpace(c.PostForm("callsign"))
	if cmd == "" {
		s.redirectSweatboxFlash(c, "err", "Command is required")
		return
	}

	payload, err := json.Marshal(server.SweatboxCommandRequest{
		Callsign: callsign,
		Command:  cmd,
	})
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "Unable to encode command")
		return
	}

	status, respBody, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/command", "application/json", bytes.NewReader(payload))
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusOK:
		var res server.SweatboxCommandResponse
		if err := json.Unmarshal(respBody, &res); err != nil {
			s.redirectSweatboxFlash(c, "err", "Unable to parse command response")
			return
		}
		if res.OK {
			msg := res.Message
			if msg == "" {
				msg = "Command accepted"
			}
			s.redirectSweatboxFlash(c, "ok", msg)
			return
		}
		msg := res.Message
		if msg == "" {
			msg = "Command rejected"
		}
		s.redirectSweatboxFlash(c, "err", msg)
	case http.StatusNotFound:
		s.redirectSweatboxFlash(c, "err", "Sweatbox is not enabled on the FSD server")
	case http.StatusConflict:
		s.redirectSweatboxFlash(c, "err", firstJSONError(respBody, "No airport loaded"))
	case http.StatusBadRequest:
		s.redirectSweatboxFlash(c, "err", "Invalid command request")
	default:
		s.redirectSweatboxFlash(c, "err", fmt.Sprintf("Command failed (HTTP %d)", status))
	}
}

// handleFrontendSweatboxPause POST /sweatbox/pause
func (s *Server) handleFrontendSweatboxPause(c *gin.Context) {
	if !s.limitSweatboxSmallForm(c) {
		return
	}
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	status, _, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/pause", "", nil)
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		s.redirectSweatboxFlash(c, "paused", "")
	case http.StatusNotFound:
		s.redirectSweatboxFlash(c, "err", "Sweatbox is not enabled on the FSD server")
	default:
		s.redirectSweatboxFlash(c, "err", fmt.Sprintf("Pause failed (HTTP %d)", status))
	}
}

// handleFrontendSweatboxUnpause POST /sweatbox/unpause
func (s *Server) handleFrontendSweatboxUnpause(c *gin.Context) {
	if !s.limitSweatboxSmallForm(c) {
		return
	}
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	status, _, err := s.fsdSweatboxDo(http.MethodPost, "/sweatbox/unpause", "", nil)
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		s.redirectSweatboxFlash(c, "unpaused", "")
	case http.StatusNotFound:
		s.redirectSweatboxFlash(c, "err", "Sweatbox is not enabled on the FSD server")
	default:
		s.redirectSweatboxFlash(c, "err", fmt.Sprintf("Unpause failed (HTTP %d)", status))
	}
}

// handleFrontendSweatboxDelete POST /sweatbox/delete
func (s *Server) handleFrontendSweatboxDelete(c *gin.Context) {
	if !s.limitSweatboxSmallForm(c) {
		return
	}
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	cs := strings.TrimSpace(c.PostForm("callsign"))
	if cs == "" {
		s.redirectSweatboxFlash(c, "err", "Callsign is required")
		return
	}
	// Path-escape callsign segments conservatively (alphanumeric callsigns expected).
	path := "/sweatbox/aircraft/" + url.PathEscape(cs)
	status, _, err := s.fsdSweatboxDo(http.MethodDelete, path, "", nil)
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		s.redirectSweatboxFlash(c, "deleted", "Deleted "+cs)
	case http.StatusNotFound:
		// Could be disabled routes or unknown callsign — prefer callsign message when possible.
		// Disabled server also 404s; surface a useful combined message.
		s.redirectSweatboxFlash(c, "err", "Aircraft not found (or sweatbox disabled): "+cs)
	default:
		s.redirectSweatboxFlash(c, "err", fmt.Sprintf("Delete failed (HTTP %d)", status))
	}
}

// handleFrontendSweatboxDeleteAll POST /sweatbox/delete-all
func (s *Server) handleFrontendSweatboxDeleteAll(c *gin.Context) {
	if !s.limitSweatboxSmallForm(c) {
		return
	}
	if !s.validateCSRF(c) {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	// Require explicit confirmation for destructive bulk action.
	if !queryTruthy(c.PostForm("confirm")) {
		s.redirectSweatboxFlash(c, "err", "Confirm delete-all by checking the confirmation box")
		return
	}
	status, _, err := s.fsdSweatboxDo(http.MethodDelete, "/sweatbox/aircraft", "", nil)
	if err != nil {
		s.redirectSweatboxFlash(c, "err", "FSD HTTP service unreachable")
		return
	}
	switch status {
	case http.StatusNoContent, http.StatusOK:
		s.redirectSweatboxFlash(c, "deleted_all", "")
	case http.StatusNotFound:
		s.redirectSweatboxFlash(c, "err", "Sweatbox is not enabled on the FSD server")
	default:
		s.redirectSweatboxFlash(c, "err", fmt.Sprintf("Delete-all failed (HTTP %d)", status))
	}
}

// limitSweatboxSmallForm caps POST body for non-upload sweatbox forms.
// Returns false when the body is too large (flash + redirect already issued).
func (s *Server) limitSweatboxSmallForm(c *gin.Context) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebSmallFormMaxBody)
	// Force parse so MaxBytesReader surfaces early for form-urlencoded.
	if err := c.Request.ParseForm(); err != nil {
		if isRequestTooLarge(err) {
			s.redirectSweatboxFlash(c, "err", "Request body too large")
			return false
		}
		// Leave form empty on other parse errors; handlers validate required fields.
	}
	return true
}

// ---------------------------------------------------------------------------
// FSD service HTTP proxy helpers
// ---------------------------------------------------------------------------

func (s *Server) fsdSweatboxDo(method, path, contentType string, body io.Reader) (status int, respBody []byte, err error) {
	client := http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()

	req, err := s.makeFsdHttpServiceHttpRequest(method, path, body)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()

	// Cap response read so a runaway FSD cannot fill memory.
	respBody, err = io.ReadAll(io.LimitReader(res.Body, sweatboxWebMaxBody+1))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, respBody, nil
}

func (s *Server) redirectSweatboxFlash(c *gin.Context, flash, msg string) {
	u := "/sweatbox?flash=" + url.QueryEscape(flash)
	if msg != "" {
		u += "&msg=" + url.QueryEscape(truncateRunes(msg, sweatboxFlashMsgMaxRunes))
	}
	c.Redirect(http.StatusSeeOther, u)
}

// truncateRunes shortens s to at most max runes, appending "…" when truncated.
func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if max == 1 {
		// Single-rune budget: prefer ellipsis over a partial character.
		r := []rune(s)
		if len(r) <= 1 {
			return s
		}
		return "…"
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// readSweatboxFormPayload prefers an uploaded file, else named text fields.
// Callers should already wrap the request body with MaxBytesReader.
func readSweatboxFormPayload(c *gin.Context, fileField string, textFields ...string) ([]byte, error) {
	ct := c.ContentType()
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := c.Request.ParseMultipartForm(sweatboxWebMaxBody); err != nil {
			return nil, err
		}
		if fh, err := c.FormFile(fileField); err == nil && fh != nil {
			f, err := fh.Open()
			if err != nil {
				return nil, err
			}
			defer f.Close()
			data, err := io.ReadAll(io.LimitReader(f, sweatboxWebMaxBody+1))
			if err != nil {
				return nil, err
			}
			if len(data) > sweatboxWebMaxBody {
				return nil, &http.MaxBytesError{Limit: sweatboxWebMaxBody}
			}
			return data, nil
		}
		for _, key := range textFields {
			if v := c.PostForm(key); v != "" {
				if len(v) > sweatboxWebMaxBody {
					return nil, &http.MaxBytesError{Limit: sweatboxWebMaxBody}
				}
				return []byte(v), nil
			}
		}
		return nil, fmt.Errorf("multipart body missing file or text field")
	}

	// application/x-www-form-urlencoded path
	if err := c.Request.ParseForm(); err != nil {
		return nil, err
	}
	for _, key := range textFields {
		if v := c.PostForm(key); v != "" {
			if len(v) > sweatboxWebMaxBody {
				return nil, &http.MaxBytesError{Limit: sweatboxWebMaxBody}
			}
			return []byte(v), nil
		}
	}
	return nil, fmt.Errorf("form missing text field")
}

func isRequestTooLarge(err error) bool {
	if err == nil {
		return false
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "http: request body too large") ||
		strings.Contains(msg, "request body too large") ||
		strings.Contains(msg, "multipart: message too large")
}

func queryTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func formatSweatboxElapsed(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	s := int(sec)
	h := s / 3600
	m := (s % 3600) / 60
	r := s % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, r)
}

func formatSweatboxInt(v float64) string {
	return strconv.Itoa(int(v + 0.5))
}

func decodeSweatboxJSONField(body []byte, key string) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case string:
			return t
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64)
		}
	}
	return ""
}

func firstJSONError(body []byte, fallback string) string {
	// Prefer message field (command responses), then errors[0], then error.
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		return fallback
	}
	if msg, ok := generic["message"].(string); ok && strings.TrimSpace(msg) != "" {
		return strings.TrimSpace(msg)
	}
	if errs, ok := generic["errors"].([]any); ok && len(errs) > 0 {
		if s, ok := errs[0].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	if e, ok := generic["error"].(string); ok && strings.TrimSpace(e) != "" {
		return strings.TrimSpace(e)
	}
	return fallback
}
