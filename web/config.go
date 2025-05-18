package main

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/renorris/openfsd/db"
	"github.com/renorris/openfsd/fsd"
	"net/http"
)

type KeyValuePair struct {
	Key   string `json:"key" binding:"required"`
	Value string `json:"value" binding:"required"`
}

func (s *Server) handleGetConfig(c *gin.Context) {
	claims := getJwtContext(c)
	if claims.NetworkRating < fsd.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	var configKeys = []string{
		db.ConfigWelcomeMessage,
		db.ConfigFsdServerHostname,
		db.ConfigFsdServerIdent,
		db.ConfigFsdServerLocation,
		db.ConfigApiServerBaseURL,
	}

	type ResponseBody struct {
		KeyValuePairs []KeyValuePair `json:"key_value_pairs" binding:"required"`
	}

	resBody := ResponseBody{
		KeyValuePairs: make([]KeyValuePair, 0, len(configKeys)),
	}

	for i := range configKeys {
		key := configKeys[i]
		val, err := s.dbRepo.ConfigRepo.Get(key)
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
	claims := getJwtContext(c)
	if claims.NetworkRating < fsd.NetworkRatingAdministator {
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

	for i := range reqBody.KeyValuePairs {
		kv := reqBody.KeyValuePairs[i]
		if err := s.dbRepo.ConfigRepo.Set(kv.Key, kv.Value); err != nil {
			res := newAPIV1Failure("Error writing key/value into persistent storage")
			writeAPIV1Response(c, http.StatusInternalServerError, &res)
			return
		}
	}

	res := newAPIV1Success(nil)
	writeAPIV1Response(c, http.StatusOK, &res)
}

func (s *Server) handleResetSecretKey(c *gin.Context) {
	claims := getJwtContext(c)
	if claims.NetworkRating < fsd.NetworkRatingAdministator {
		writeAPIV1Response(c, http.StatusForbidden, &genericAPIV1Forbidden)
		return
	}

	secretKey, err := db.GenerateJwtSecretKey()
	if err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	if err = s.dbRepo.ConfigRepo.Set(db.ConfigJwtSecretKey, string(secretKey[:])); err != nil {
		writeAPIV1Response(c, http.StatusInternalServerError, &genericAPIV1InternalServerError)
		return
	}

	res := newAPIV1Success(nil)
	writeAPIV1Response(c, http.StatusOK, &res)
}
