package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/renorris/openfsd/protocol"
	"golang.org/x/crypto/bcrypt"
	"log"
	"net/http"
	"strconv"
	"time"
)

//go:embed dashboard.html
var dashboardHtml []byte

type JwtRequest struct {
	CID      string `json:"cid"`
	Password string `json:"password"`
}

type JwtResponse struct {
	Success  bool   `json:"success"`
	Token    string `json:"token,omitempty"`
	ErrorMsg string `json:"error_msg,omitempty"`
}

type CustomClaims struct {
	jwt.RegisteredClaims
	ControllerRating int `json:"controller_rating"`
	PilotRating      int `json:"pilot_rating"`
}

type UserApiResponse struct {
	Success bool           `json:"success"`
	Message string         `json:"msg"`
	User    *FSDUserRecord `json:"user_record,omitempty"`
}

func fsdJwtApiHandler(w http.ResponseWriter, r *http.Request) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var jwtRequest JwtRequest
	if err = json.Unmarshal(buf.Bytes(), &jwtRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	cid, err := strconv.Atoi(jwtRequest.CID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	userRecord, userRecordErr := GetUserRecord(DB, cid)
	if userRecordErr != nil && !errors.Is(userRecordErr, sql.ErrNoRows) {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// If user not found
	if errors.Is(userRecordErr, sql.ErrNoRows) {
		jwtResponse := JwtResponse{
			Success:  false,
			Token:    "",
			ErrorMsg: "User not found",
		}

		body, marshalErr := json.Marshal(jwtResponse)
		if marshalErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_, writeErr := w.Write(body)
		if writeErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		return
	}

	// Verify password
	userRecordErr = bcrypt.CompareHashAndPassword([]byte(userRecord.Password), []byte(jwtRequest.Password))
	if userRecordErr != nil { // Password didn't match
		jwtResponse := JwtResponse{
			Success:  false,
			Token:    "",
			ErrorMsg: "Password is incorrect",
		}

		body, err := json.Marshal(jwtResponse)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_, writeErr := w.Write(body)
		if writeErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		return
	}

	// Else send a login token
	idBytes := make([]byte, 16)
	_, userRecordErr = rand.Read(idBytes)
	if userRecordErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	idStr := base64.StdEncoding.EncodeToString(idBytes)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, CustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "openfsd",
			Subject:   jwtRequest.CID,
			Audience:  []string{"fsd-live"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(420 * time.Second)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-120 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        idStr,
		},
		ControllerRating: userRecord.Rating,
		PilotRating:      0,
	})

	tokenString, userRecordErr := token.SignedString(JWTKey)
	if userRecordErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	jwtResponse := JwtResponse{
		Success:  true,
		Token:    tokenString,
		ErrorMsg: "",
	}

	body, userRecordErr := json.Marshal(jwtResponse)
	if userRecordErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, writeErr := w.Write(body)
	if writeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func userAPIHandler(w http.ResponseWriter, r *http.Request) {

	// Check for a valid token cookie
	cookie, err := r.Cookie("token")
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Validate token
	claims := jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(cookie.Value, &claims, func(token *jwt.Token) (interface{}, error) {
		return JWTKey, nil
	})

	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	audience, err := token.Claims.GetAudience()
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	audienceValid := false
	for _, audClaim := range audience {
		if audClaim == "administrator-dashboard" {
			audienceValid = true
			break
		}
	}

	if !audienceValid {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Handle the request
	switch r.Method {
	case "GET":
		cidStr := r.FormValue("cid")

		cid, err := strconv.Atoi(cidStr)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		userRecord, err := GetUserRecord(DB, cid)
		if err != nil {
			res := UserApiResponse{
				Success: false,
				Message: "Error: user not found",
			}

			resBytes, err := json.Marshal(res)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			w.Header().Add("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(resBytes)
			return
		}

		// omit password
		userRecord.Password = ""

		res := UserApiResponse{
			Success: true,
			Message: "Success",
			User:    userRecord,
		}

		resBytes, err := json.Marshal(res)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(resBytes)
		return
	case "POST":
		buf := new(bytes.Buffer)
		_, err := buf.ReadFrom(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		req := FSDUserRecord{}
		err = json.Unmarshal(buf.Bytes(), &req)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		bcryptBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
		passwordHash := string(bcryptBytes)

		record, err := AddUserRecordSequential(DB, passwordHash, req.Rating, req.RealName)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		record.Password = ""

		res := UserApiResponse{
			Success: true,
			Message: "Success: added user with CID " + fmt.Sprintf("%d", record.CID),
			User:    record,
		}

		resBytes, err := json.Marshal(res)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(resBytes)
		return
	case "PATCH":
		buf := new(bytes.Buffer)
		_, err := buf.ReadFrom(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		req := FSDUserRecord{}
		err = json.Unmarshal(buf.Bytes(), &req)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if len(req.Password) > 0 {
			bcryptBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			req.Password = string(bcryptBytes)
		}

		err = UpdateUserRecord(DB, &req)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		req.Password = ""

		res := UserApiResponse{
			Success: true,
			Message: "Success: updated user with CID " + fmt.Sprintf("%d", req.CID),
			User:    &req,
		}

		resBytes, err := json.Marshal(res)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(resBytes)
		return
	}
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {

	// If we have a valid cookie containing a valid JWT, send the dashboard page
	cookie, err := r.Cookie("token")
	if err == nil {
		claims := jwt.RegisteredClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, &claims, func(token *jwt.Token) (interface{}, error) {
			return JWTKey, nil
		})

		// If the token is invalid, remove the cookie and send back basic auth
		if err != nil {
			cookie.Expires = time.Unix(0, 0)
			cookie.Value = ""
			http.SetCookie(w, cookie)

			w.Header().Add("WWW-Authenticate", `Basic realm="dashboard", charset="UTF-8"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		audience, err := token.Claims.GetAudience()
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		audienceValid := false
		for _, audClaim := range audience {
			if audClaim == "administrator-dashboard" {
				audienceValid = true
				break
			}
		}

		if !audienceValid {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write(dashboardHtml)
		return
	}

	// If we don't have a cookie, check if we were sent basic auth information
	username, pwd, ok := r.BasicAuth()

	// If not, send the basic auth header
	if !ok {
		w.Header().Add("WWW-Authenticate", `Basic realm="dashboard", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	cid, err := strconv.Atoi(username)
	if err != nil {
		w.Header().Add("WWW-Authenticate", `Basic realm="dashboard", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	userRecord, err := GetUserRecord(DB, cid)
	if err != nil {
		w.Header().Add("Content-Type", "text/plain")
		w.Header().Add("WWW-Authenticate", `Basic realm="dashboard", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("user not found"))
		return
	}

	if userRecord.Rating < protocol.NetworkRatingSUP {
		w.Header().Add("Content-Type", "text/plain")
		w.Header().Add("WWW-Authenticate", `Basic realm="dashboard", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("rating too low"))
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(userRecord.Password), []byte(pwd))
	if err != nil {
		w.Header().Add("Content-Type", "text/plain")
		w.Header().Add("WWW-Authenticate", `Basic realm="dashboard", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("user not found"))
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "openfsd",
		Subject:   fmt.Sprintf("%d", userRecord.CID),
		Audience:  []string{"administrator-dashboard"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(12 * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	})

	tokenStr, err := token.SignedString(JWTKey)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:    "token",
		Value:   tokenStr,
		Expires: time.Now().Add(12 * time.Hour),
	})

	w.WriteHeader(http.StatusOK)
	w.Write(dashboardHtml)
}

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusForbidden)
}

func StartHttpServer(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/fsd-jwt", fsdJwtApiHandler)
	mux.HandleFunc("GET /dashboard", dashboardHandler)
	mux.HandleFunc("/user", userAPIHandler)
	mux.HandleFunc("/", defaultHandler)
	server := &http.Server{Addr: SC.HttpListenAddr, Handler: mux}
	go func() {
		if SC.HttpsEnabled {
			if err := server.ListenAndServeTLS(SC.TLSCertFile, SC.TLSKeyFile); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					log.Fatal("https server error:\n" + err.Error())
				}
			}
		} else {
			if err := server.ListenAndServe(); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					log.Fatal("http server error:\n" + err.Error())
				}
			}
		}

	}()

	log.Println("HTTP listening")

	// Wait for context done signal
	<-ctx.Done()

	// Shutdown server
	if err := server.Shutdown(context.Background()); err != nil {
		log.Fatal("http server shutdown error:\n" + err.Error())
	}
}
