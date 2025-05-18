package main

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/fsd"
	"net/http"
)

func (s *Server) handleKickActiveConnection(c *gin.Context) {
	claims := getJwtContext(c)
	if claims.NetworkRating < fsd.NetworkRatingSupervisor {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	type RequestBody struct {
		Callsign string `json:"callsign" binding:"required"`
	}

	var reqBody RequestBody
	if !bindJSONOrAbort(c, &reqBody) {
		return
	}

	buf := bytes.Buffer{}
	if err := json.NewEncoder(&buf).Encode(reqBody); err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	client := http.Client{}
	defer client.CloseIdleConnections()
	req, err := s.makeFsdHttpServiceHttpRequest("POST", "/kick_user", &buf)
	if err != nil {
		return
	}
	res, err := client.Do(req)
	if err != nil {
		return
	}

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
