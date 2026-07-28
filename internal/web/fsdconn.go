package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/pkg/protocol"
)

func (s *Server) handleKickActiveConnection(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	if claims.NetworkRating < protocol.NetworkRatingSupervisor {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	type RequestBody struct {
		Callsign string `json:"callsign" binding:"required"`
		NodeID   string `json:"node_id"` // optional; routes kick to hosting FSD edge
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	client := http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	// Multi-FSD: route by node_id when present.
	if len(s.fsdServiceURLs()) > 1 || reqBody.NodeID != "" {
		status, err := s.kickUserOnNode(&client, reqBody.Callsign, reqBody.NodeID)
		if err != nil && status == 0 {
			writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
			return
		}
		switch status {
		case http.StatusNoContent, http.StatusOK:
			apiV1Res := newAPIV1Success(nil)
			writeAPIV1Response(c, http.StatusOK, &apiV1Res)
			return
		case http.StatusNotFound:
			apiV1Res := newAPIV1Failure("Callsign not found")
			writeAPIV1Response(c, http.StatusNotFound, &apiV1Res)
			return
		default:
			writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
			return
		}
	}

	buf := bytes.Buffer{}
	if err := json.NewEncoder(&buf).Encode(map[string]string{"callsign": reqBody.Callsign}); err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}
	req, err := s.makeFsdHttpServiceHttpRequest("POST", "/kick_user", &buf)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}
	res, err := client.Do(req)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusNoContent:
		apiV1Res := newAPIV1Success(nil)
		writeAPIV1Response(c, http.StatusOK, &apiV1Res)
		return
	case http.StatusNotFound:
		apiV1Res := newAPIV1Failure("Callsign not found")
		writeAPIV1Response(c, http.StatusNotFound, &apiV1Res)
		return
	default:
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}
}
