package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/pkg/protocol"
)

type KeyValuePair struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value" binding:"required"`
}

func (s *Server) handleGetConfig(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	if claims.NetworkRating < protocol.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	var configKeys = []string{
		db.ConfigWelcomeMessage,
		db.ConfigFsdServerHostname,
		db.ConfigFsdServerIdent,
		db.ConfigFsdServerLocation,
		db.ConfigApiServerBaseURL,
		db.ConfigRequirePilotPPL,
	}

	type ResponseBody struct {
		KeyValuePairs []KeyValuePair `json:"key_value_pairs" binding:"required"`
	}

	resBody := ResponseBody{
		KeyValuePairs: make([]KeyValuePair, 0, len(configKeys)),
	}

	for i := range configKeys {
		key := configKeys[i]
		val, err := s.dbRepo.ConfigRepo.Get(context.Background(), key)
		if err != nil {
			if !errors.Is(err, db.ErrConfigKeyNotFound) {
				res := newAPIV1Failure("Error reading key/value from persistent storage")
				writeAPIV1Response(c, http.StatusInternalServerError, &res)
				return
			}
			continue
		}
		resBody.KeyValuePairs = append(resBody.KeyValuePairs,
			KeyValuePair{
				Key:   key,
				Value: val,
			},
		)
	}

	res := newAPIV1Success(&resBody)
	writeAPIV1Response(c, http.StatusOK, &res)
}

func (s *Server) handleUpdateConfig(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	if claims.NetworkRating < protocol.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	type RequestBody struct {
		KeyValuePairs []KeyValuePair `json:"key_value_pairs" binding:"required"`
	}

	var reqBody RequestBody
	if ok := bindJSONOrAbort(c, &reqBody); !ok {
		return
	}

	// Best-effort multi-key write (no multi-Set transaction on ConfigRepo).
	for i := range reqBody.KeyValuePairs {
		kv := reqBody.KeyValuePairs[i]
		if !isEditableConfigKey(kv.Key) {
			res := newAPIV1Failure("unknown or non-editable config key")
			writeAPIV1Response(c, http.StatusBadRequest, &res)
			return
		}
		if err := s.dbRepo.ConfigRepo.Set(context.Background(), kv.Key, kv.Value); err != nil {
			res := newAPIV1Failure("Error writing key/value into persistent storage")
			writeAPIV1Response(c, http.StatusInternalServerError, &res)
			return
		}
	}

	res := newAPIV1Success(nil)
	writeAPIV1Response(c, http.StatusOK, &res)
}

func (s *Server) handleResetSecretKey(c *gin.Context) {
	claims, ok := requireJwtContext(c)
	if !ok {
		return
	}
	if claims.NetworkRating < protocol.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	secretKey, err := db.GenerateJwtSecretKey()
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	if err = s.dbRepo.ConfigRepo.Set(context.Background(), db.ConfigJwtSecretKey, secretKey); err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	res := newAPIV1Success(nil)
	writeAPIV1Response(c, http.StatusOK, &res)
}
