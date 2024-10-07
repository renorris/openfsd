package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	auth2 "github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"io"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
)

type APIV1UsersRequest struct {
	CID  int                    `json:"cid,omitempty"`
	User database.FSDUserRecord `json:"user,omitempty"`
}

type APIV1UsersResponse struct {
	StatusMessage string                  `json:"msg"`
	User          *database.FSDUserRecord `json:"user,omitempty"`
}

// APIV1UsersHandler handles all /api/v1/users calls
func APIV1UsersHandler(w http.ResponseWriter, r *http.Request, verifier auth2.JWTVerifier) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)

	w.Header().Set("Content-Type", "application/json")
	resp := APIV1UsersResponse{}

	// Verify authorization
	var tokenStr string
	if tokenStr = r.Header.Get("Authorization"); tokenStr == "" {
		resp.StatusMessage = "authorization header missing"
		writeResponseError(w, http.StatusBadRequest, &resp)
		return
	}

	if split := strings.Split(tokenStr, "Bearer "); len(split) != 2 {
		resp.StatusMessage = "invalid authorization header format"
		writeResponseError(w, http.StatusBadRequest, &resp)
		return
	} else {
		tokenStr = split[1]
	}

	// Verify JWT signature and expiry times
	var token *jwt.Token
	var err error
	if token, err = verifier.VerifyJWT(tokenStr); err != nil {
		resp.StatusMessage = "invalid token"
		writeResponseError(w, http.StatusForbidden, &resp)
		return
	}

	// Verify JWT claims
	claims := auth2.FSDJWTClaims{}
	if err = claims.Parse(token); err != nil {
		resp.StatusMessage = "invalid token claims"
		writeResponseError(w, http.StatusForbidden, &resp)
		return
	}

	if !slices.Contains(claims.Audience(), "dashboard") {
		resp.StatusMessage = "invalid token audience"
		writeResponseError(w, http.StatusForbidden, &resp)
		return
	}

	// Read body
	var body []byte
	if body, err = io.ReadAll(r.Body); err != nil {
		resp.StatusMessage = "error reading request body"
		writeResponseError(w, http.StatusInternalServerError, &resp)
		return
	}

	var status int
	req := APIV1UsersRequest{}
	// GET method doesn't have a body, handle it separately
	if r.Method == "GET" {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
			resp.StatusMessage = "invalid request path"
			writeResponseError(w, http.StatusBadRequest, &resp)
			return
		}

		var cid int
		if cid, err = strconv.Atoi(path.Base(r.URL.Path)); err != nil {
			resp.StatusMessage = "invalid CID"
			writeResponseError(w, http.StatusBadRequest, &resp)
			return
		}
		req.CID = cid
		status = getUserHandler(&claims, &req, &resp)
	} else {
		if err = json.Unmarshal(body, &req); err != nil {
			resp.StatusMessage = "error parsing request body"
			writeResponseError(w, http.StatusBadRequest, &resp)
			return
		}

		switch r.Method {
		case "POST":
			status = createUserHandler(&claims, &req, &resp)
		case "PUT":
			status = updateUserHandler(&claims, &req, &resp)
		case "DELETE":
			status = deleteUserHandler(&claims, &req, &resp)
		default:
			resp.StatusMessage = "method not allowed"
			writeResponseError(w, http.StatusMethodNotAllowed, &resp)
			return
		}
	}

	// Serialize response
	var resBody []byte
	if resBody, err = json.Marshal(&resp); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp.StatusMessage = "error serializing response body"
		if respBytes, err := json.Marshal(&resp); err == nil {
			io.Copy(w, bytes.NewReader(respBytes))
		}
		return
	}

	w.WriteHeader(status)
	io.Copy(w, bytes.NewReader(resBody))
}

func createUserHandler(claims *auth2.FSDJWTClaims, req *APIV1UsersRequest, res *APIV1UsersResponse) (status int) {
	// User must be an administrator, or a supervisor with the limitation of only creating users of lower rating
	if claims.ControllerRating() != protocol.NetworkRatingADM {
		if claims.ControllerRating() < protocol.NetworkRatingSUP {
			res.StatusMessage = "must be at least Supervisor to create user"
			return http.StatusForbidden
		}
		if req.User.NetworkRating >= protocol.NetworkRatingSUP {
			res.StatusMessage = "created user must be below supervisor rating"
			return http.StatusForbidden
		}
	}

	var err error
	var autoPassword, fsdAutoPassword bool
	if req.User.Password == "" {
		if req.User.Password, err = generateRandomPassword(); err != nil {
			res.StatusMessage = "error generating random password"
			return http.StatusInternalServerError
		}
		autoPassword = true
	}
	if req.User.FSDPassword == "" {
		if req.User.FSDPassword, err = generateRandomPassword(); err != nil {
			res.StatusMessage = "error generating random password"
			return http.StatusInternalServerError
		}
		fsdAutoPassword = true
	}

	if req.User.CID, err = req.User.Insert(servercontext.DB()); err != nil {
		res.StatusMessage = "error inserting user into database"
		return http.StatusInternalServerError
	}

	res.StatusMessage = "success"

	// omit passwords for response if they weren't auto generated
	if !autoPassword {
		req.User.Password = ""
	}
	if !fsdAutoPassword {
		req.User.FSDPassword = ""
	}

	// copy user into response
	res.User = &req.User

	return http.StatusOK
}

func getUserHandler(claims *auth2.FSDJWTClaims, req *APIV1UsersRequest, res *APIV1UsersResponse) (status int) {
	if claims.ControllerRating() < protocol.NetworkRatingSUP {
		res.StatusMessage = "must be at least Supervisor to read users"
		return http.StatusForbidden
	}

	userRecord := database.FSDUserRecord{}
	if err := userRecord.LoadByCID(servercontext.DB(), req.CID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			res.StatusMessage = "no user found"
			return http.StatusNotFound
		}

		res.StatusMessage = "error loading user from database"
		return http.StatusInternalServerError
	}

	// omit passwords
	userRecord.Password = ""
	userRecord.FSDPassword = ""

	res.StatusMessage = "success"

	// copy into response
	res.User = &userRecord

	return http.StatusOK
}

func updateUserHandler(claims *auth2.FSDJWTClaims, req *APIV1UsersRequest, res *APIV1UsersResponse) (status int) {
	// User must be an administrator, or a supervisor with the limitation of only updating users of lower rating
	if claims.ControllerRating() != protocol.NetworkRatingADM {
		if claims.ControllerRating() < protocol.NetworkRatingSUP {
			res.StatusMessage = "must be at least Supervisor to update user"
			return http.StatusForbidden
		}
		if req.User.NetworkRating >= protocol.NetworkRatingSUP {
			res.StatusMessage = "user to update must be below supervisor rating"
			return http.StatusForbidden
		}
	}

	var err error
	if err = req.User.Update(servercontext.DB()); err != nil {
		if errors.Is(err, database.NoRowsChangedError) {
			res.StatusMessage = "user not found"
			return http.StatusNotFound
		}
		res.StatusMessage = "error updating user"
		return http.StatusInternalServerError
	}

	if err = req.User.LoadByCID(servercontext.DB(), req.User.CID); err != nil {
		res.StatusMessage = "error loading user for response"
		return http.StatusInternalServerError
	}

	res.StatusMessage = "success"

	// omit passwords for response
	req.User.Password = ""
	req.User.FSDPassword = ""

	// copy user into response
	res.User = &req.User

	return http.StatusOK
}

func deleteUserHandler(claims *auth2.FSDJWTClaims, req *APIV1UsersRequest, res *APIV1UsersResponse) (status int) {
	// User must be an administrator
	if claims.ControllerRating() != protocol.NetworkRatingADM {
		res.StatusMessage = "must be at Administrator to delete user"
		return http.StatusForbidden
	}

	var err error
	if err = req.User.Delete(servercontext.DB(), req.CID); err != nil {
		if errors.Is(err, database.NoRowsChangedError) {
			res.StatusMessage = "user not found"
			return http.StatusNotFound
		}
		res.StatusMessage = "error deleting user"
		return http.StatusInternalServerError
	}

	res.StatusMessage = "success"
	res.User = nil

	return http.StatusOK
}
