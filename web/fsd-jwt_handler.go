package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// FSDJWTHandler administers tokens for base-level privilege FSD server connections
func FSDJWTHandler(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 256)

	w.Header().Set("Content-Type", "application/json")
	resp := auth.FSDJWTResponse{}
	var respBytes []byte

	// Load the request body
	var body []byte
	var err error
	if body, err = io.ReadAll(r.Body); err != nil {
		resp.Success, resp.ErrorMsg = false, "error reading request body"
		writeResponseError(w, http.StatusBadRequest, &resp)
		return
	}

	// Parse the request body
	var req auth.FSDJWTRequest
	if err = json.Unmarshal(body, &req); err != nil {
		resp.Success, resp.ErrorMsg = false, "invalid request body"
		writeResponseError(w, http.StatusBadRequest, &resp)
		return
	}

	// Parse CID integer
	var cidInt int
	if cidInt, err = strconv.Atoi(req.CID); err != nil {
		resp.Success, resp.ErrorMsg = false, "invalid CID"
		writeResponseError(w, http.StatusBadRequest, &resp)
		return
	}

	// Load user record from database
	userRecord := database.FSDUserRecord{}
	if err = userRecord.LoadByCID(servercontext.DB(), cidInt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			resp.Success, resp.ErrorMsg = false, "invalid CID"
			writeResponseError(w, http.StatusUnauthorized, &resp)
			return
		}

		resp.Success, resp.ErrorMsg = false, "internal server error"
		writeResponseError(w, http.StatusInternalServerError, &resp)
		return
	}

	// Verify password hash
	if err = bcrypt.CompareHashAndPassword([]byte(userRecord.FSDPassword), []byte(req.Password)); err != nil {
		resp.Success, resp.ErrorMsg = false, "invalid credentials"
		writeResponseError(w, http.StatusUnauthorized, &resp)
		return
	}

	// Verify account standing
	if userRecord.NetworkRating <= protocol.NetworkRatingSUS {
		resp.Success, resp.ErrorMsg = false, "account suspended/inactive"
		writeResponseError(w, http.StatusForbidden, &resp)
		return
	}

	// All good. Administer the token.
	// use "fsd" audience to specify that this token is only valid for connecting to FSD.
	claims := auth.NewFSDJWTClaims(userRecord.CID, userRecord.NetworkRating, userRecord.PilotRating, []string{"fsd"})

	var token string
	if token, err = claims.MakeToken(time.Now().Add(420 * time.Second)); err != nil {
		resp.Success, resp.ErrorMsg = false, "internal server error"
		writeResponseError(w, http.StatusInternalServerError, &resp)
		return
	}

	resp.Success, resp.Token = true, token
	if respBytes, err = json.Marshal(resp); err == nil {
		io.Copy(w, bytes.NewReader(respBytes))
	}
}
