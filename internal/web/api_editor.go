package web

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/renorris/openfsd/pkg/twrfiles"
)

// editorValidateRequest is the JSON body for POST /api/v1/editor/validate-*.
// Contract: Content-Type application/json, field "text" holds the full .apt/.air file text.
// Max raw request body size is sweatboxWebMaxBody (2 MiB).
type editorValidateRequest struct {
	Text string `json:"text"`
}

// editorValidateAPTData is the success data payload for validate-apt.
// Soft parse errors are listed in Errors; full geometry is never returned.
type editorValidateAPTData struct {
	Errors       []string `json:"errors"`
	ICAO         string   `json:"icao"`
	SurfaceCount int      `json:"surface_count"`
}

// editorValidateAIRData is the success data payload for validate-air.
// Soft parse errors are listed in Errors; aircraft rows are never returned.
type editorValidateAIRData struct {
	Errors        []string `json:"errors"`
	AircraftCount int      `json:"aircraft_count"`
}

// setupEditorAPIRoutes mounts Instructor1+ APT/AIR validate endpoints under /api/v1/editor.
// Dual-accept Bearer | session cookie; CSRF when cookie-authenticated.
// Authz is inline I1+ → 403 JSON (never HTML redirect).
func (s *Server) setupEditorAPIRoutes(parent *gin.RouterGroup) {
	g := parent.Group("/editor")
	s.useAPIV1Protected(g)
	g.POST("/validate-apt", s.handleAPIValidateAPT)
	g.POST("/validate-air", s.handleAPIValidateAIR)
}

// handleAPIValidateAPT POST /api/v1/editor/validate-apt
//
// Stateless ParseAPT of JSON {"text":"…"}. Instructor1+ only; 403 JSON when rating too low.
// Soft validation errors are returned in data.errors (HTTP 200); never returns geometry.
func (s *Server) handleAPIValidateAPT(c *gin.Context) {
	claims := getJwtContext(c)
	if claims == nil || claims.NetworkRating < protocol.NetworkRatingInstructor1 {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	text, ok := readEditorValidateText(c)
	if !ok {
		return
	}

	apt, errs := twrfiles.ParseAPT(text)
	if errs == nil {
		errs = []string{}
	}

	slog.Info("editor validate-apt",
		"cid", claims.CID,
		"content_length", len(text),
		"icao", apt.ICAO,
		"surface_count", len(apt.Surfaces),
		"error_count", len(errs),
	)

	res := newAPIV1Success(&editorValidateAPTData{
		Errors:       errs,
		ICAO:         apt.ICAO,
		SurfaceCount: len(apt.Surfaces),
	})
	writeAPIV1Response(c, http.StatusOK, &res)
}

// handleAPIValidateAIR POST /api/v1/editor/validate-air
//
// Stateless ParseAIR of JSON {"text":"…"}. Instructor1+ only; 403 JSON when rating too low.
// Soft validation errors are returned in data.errors (HTTP 200); never returns aircraft rows.
func (s *Server) handleAPIValidateAIR(c *gin.Context) {
	claims := getJwtContext(c)
	if claims == nil || claims.NetworkRating < protocol.NetworkRatingInstructor1 {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	text, ok := readEditorValidateText(c)
	if !ok {
		return
	}

	rows, errs := twrfiles.ParseAIR(text)
	if errs == nil {
		errs = []string{}
	}

	slog.Info("editor validate-air",
		"cid", claims.CID,
		"content_length", len(text),
		"aircraft_count", len(rows),
		"error_count", len(errs),
	)

	res := newAPIV1Success(&editorValidateAIRData{
		Errors:        errs,
		AircraftCount: len(rows),
	})
	writeAPIV1Response(c, http.StatusOK, &res)
}

// readEditorValidateText binds JSON {"text":"…"} with a 2 MiB raw body cap.
// On failure it writes an APIV1 error response and returns ok=false.
func readEditorValidateText(c *gin.Context) (text string, ok bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sweatboxWebMaxBody)

	var req editorValidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if isRequestTooLarge(err) {
			res := newAPIV1Failure("request body too large (max 2 MiB)")
			writeAPIV1Response(c, http.StatusRequestEntityTooLarge, &res)
			return "", false
		}
		res := newAPIV1Failure("invalid JSON body")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return "", false
	}
	return req.Text, true
}
