package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
)

// handleAPISweatboxState GET /api/v1/sweatbox/state
//
// Authenticated read proxy of FSD service HTTP GET /sweatbox/state.
// Used by sweatbox.js progressive enhancement (1–2 s poll). Cookie session
// or Bearer; CSRF not required for GET. Min rating Administrator.
//
// On success (and FSD 404 disabled), the FSD JSON body is passed through so
// the PE client can use the same shape as the service control plane.
func (s *Server) handleAPISweatboxState(c *gin.Context) {
	claims := getJwtContext(c)
	if claims == nil || claims.NetworkRating < protocol.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	status, body, err := s.fsdSweatboxDo(http.MethodGet, "/sweatbox/state", "", nil)
	if err != nil {
		res := newAPIV1Failure("FSD HTTP service unreachable")
		writeAPIV1Response(c, http.StatusBadGateway, &res)
		return
	}

	switch status {
	case http.StatusOK, http.StatusNotFound:
		// 404 = sweatbox disabled on FSD (no /sweatbox/* routes).
		c.Data(status, "application/json", body)
	case http.StatusUnauthorized, http.StatusForbidden:
		res := newAPIV1Failure("FSD service rejected the request")
		writeAPIV1Response(c, http.StatusBadGateway, &res)
	default:
		res := newAPIV1Failure("FSD service returned unexpected status")
		writeAPIV1Response(c, http.StatusBadGateway, &res)
	}
}

// handleAPISweatboxOps GET /api/v1/sweatbox/ops
//
// Read proxy of FSD GET /sweatbox/ops (elapsed / arr / dep / ops-per-min).
// Optional for PE; state already includes most of these fields.
func (s *Server) handleAPISweatboxOps(c *gin.Context) {
	claims := getJwtContext(c)
	if claims == nil || claims.NetworkRating < protocol.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	status, body, err := s.fsdSweatboxDo(http.MethodGet, "/sweatbox/ops", "", nil)
	if err != nil {
		res := newAPIV1Failure("FSD HTTP service unreachable")
		writeAPIV1Response(c, http.StatusBadGateway, &res)
		return
	}

	switch status {
	case http.StatusOK, http.StatusNotFound:
		c.Data(status, "application/json", body)
	default:
		res := newAPIV1Failure("FSD service returned unexpected status")
		writeAPIV1Response(c, http.StatusBadGateway, &res)
	}
}
