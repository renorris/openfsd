package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/pkg/protocol"
)

func (s *Server) handleCreateNewAPIToken(c *gin.Context) {
	claims := getJwtContext(c)
	if claims.NetworkRating < protocol.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	type RequestBody struct {
		ExpiryDateTime time.Time `json:"expiry_date_time" time_format:"2006-01-02T15:04:05.000Z" binding:"required"`
	}

	var reqBody RequestBody
	if ok := bindJSONOrAbort(c, &reqBody); !ok {
		return
	}

	now := time.Now()

	if reqBody.ExpiryDateTime.Before(now) {
		res := newAPIV1Failure("expiry_date_time cannot be in the past")
		writeAPIV1Response(c, http.StatusBadRequest, &res)
		return
	}

	validityDuration := reqBody.ExpiryDateTime.Sub(now)

	accessToken, err := auth.MakeJwtToken(&auth.CustomFields{
		TokenType:     "access",
		CID:           claims.CID,
		NetworkRating: protocol.NetworkRatingAdministator,
	}, validityDuration)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	secretKey, err := s.dbRepo.ConfigRepo.Get(db.ConfigJwtSecretKey)
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	accessTokenStr, err := accessToken.SignedString([]byte(secretKey))
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	type ResponseBody struct {
		Token string `json:"token"`
	}

	resBody := ResponseBody{Token: accessTokenStr}
	res := newAPIV1Success(&resBody)
	writeAPIV1Response(c, http.StatusCreated, &res)
}
