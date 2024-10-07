package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/renorris/openfsd/auth"
	"github.com/renorris/openfsd/bootstrap"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"github.com/renorris/openfsd/web"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/database"
	"github.com/stretchr/testify/assert"
)

// MockVerifier is a mock implementation of JWTVerifier for testing
type MockVerifier struct {
	Claims *auth.FSDJWTClaims
	Err    error
}

func (mv *MockVerifier) VerifyJWT(tokenStr string) (token *jwt.Token, err error) {
	if mv.Err != nil {
		return nil, mv.Err
	}
	if mv.Claims != nil {
		// Construct the token using the mock claims
		now := time.Now()
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.FSDJWTCustomClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "openfsd",
				Subject:   strconv.Itoa(mv.Claims.CID()),
				Audience:  mv.Claims.Audience(),
				ExpiresAt: jwt.NewNumericDate(now.Add(420 * time.Second)),
				NotBefore: jwt.NewNumericDate(now.Add(-120 * time.Second)),
				IssuedAt:  jwt.NewNumericDate(now),
				ID:        "randomrandomrandom",
			},
			ControllerRating: int(mv.Claims.ControllerRating()),
			PilotRating:      int(mv.Claims.PilotRating()),
		})

		if tokenStr, err = t.SignedString(servercontext.JWTKey()); err != nil {
			panic(err)
		}

		if token, err = jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return servercontext.JWTKey(), nil
		}, jwt.WithValidMethods([]string{"HS256"})); err != nil {
			panic(err)
		}

		return token, nil
	}
	return nil, errors.New("invalid token")
}

func TestAPIV1UsersHandler(t *testing.T) {

	if err := os.Setenv("IN_MEMORY_DB", "true"); err != nil {
		t.Fatal(err)
	}

	// Start the server
	ctx, cancelCtx := context.WithCancel(context.Background())
	b := bootstrap.NewDefaultBootstrap()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Add demo user
	demoRecord := database.FSDUserRecord{
		Email:         "example@mail.com",
		FirstName:     "Test user 666",
		LastName:      "Test user 666 lastname",
		Password:      "12345",
		FSDPassword:   "54321",
		NetworkRating: 1,
		PilotRating:   0,
	}

	// Insert it
	var err error
	if demoRecord.CID, err = demoRecord.Insert(servercontext.DB()); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name             string
		method           string
		authorization    string
		requestBody      web.APIV1UsersRequest
		expectedStatus   int
		expectedErrorMsg string
		verifier         *MockVerifier
	}{
		{
			name:             "Missing Authorization",
			method:           "POST",
			expectedStatus:   http.StatusBadRequest,
			expectedErrorMsg: "authorization header missing",
		},
		{
			name:             "Invalid Bearer Token Format",
			method:           "POST",
			authorization:    "Basic token",
			expectedStatus:   http.StatusBadRequest,
			expectedErrorMsg: "invalid authorization header format",
		},
		{
			name:             "Invalid Token",
			method:           "POST",
			authorization:    "Bearer invalid_token",
			expectedStatus:   http.StatusForbidden,
			expectedErrorMsg: "invalid token",
			verifier:         &MockVerifier{Claims: nil, Err: nil},
		},
		{
			name:           "Successful User Creation",
			method:         "POST",
			authorization:  "Bearer valid_token",
			requestBody:    web.APIV1UsersRequest{User: database.FSDUserRecord{CID: 1, NetworkRating: 1}},
			expectedStatus: http.StatusOK,
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingADM, protocol.PilotRatingPPL, []string{"dashboard"}),
			},
		},
		{
			name:           "Successful User Load",
			method:         "GET",
			authorization:  "Bearer valid_token",
			requestBody:    web.APIV1UsersRequest{CID: demoRecord.CID},
			expectedStatus: http.StatusOK,
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingADM, protocol.PilotRatingPPL, []string{"dashboard"}),
			},
		},
		{
			name:          "Successful User Update",
			method:        "PUT",
			authorization: "Bearer valid_token",
			requestBody: web.APIV1UsersRequest{User: database.FSDUserRecord{
				CID:           demoRecord.CID,
				Email:         "newemail@example.com",
				FirstName:     "new first name",
				LastName:      "new last name",
				Password:      "newpassword",
				FSDPassword:   "newfsdpassword",
				NetworkRating: 2,
				PilotRating:   0,
			}},
			expectedStatus: http.StatusOK,
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingADM, protocol.PilotRatingPPL, []string{"dashboard"}),
			},
		},
		{
			name:             "Successful User Creation, but request is too long (> 8192 bytes)",
			method:           "POST",
			authorization:    "Bearer valid_token",
			requestBody:      web.APIV1UsersRequest{User: database.FSDUserRecord{CID: 1, NetworkRating: 1, Email: strings.Repeat("b", 16384)}},
			expectedStatus:   http.StatusInternalServerError,
			expectedErrorMsg: "error reading request body",
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingADM, protocol.PilotRatingPPL, []string{"dashboard"}),
			},
		},
		{
			name:             "Forbidden User Creation by Non-Supervisor",
			method:           "POST",
			authorization:    "Bearer valid_token",
			requestBody:      web.APIV1UsersRequest{User: database.FSDUserRecord{CID: 2, NetworkRating: 3}}, // Not enough rating to create user
			expectedStatus:   http.StatusForbidden,
			expectedErrorMsg: "must be at least Supervisor to create user",
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingS1, protocol.PilotRatingPPL, []string{"dashboard"}), // Not a Supervisor
			},
		},
		{
			name:             "Forbidden User Load by Non-Supervisor",
			method:           "GET",
			authorization:    "Bearer valid_token",
			requestBody:      web.APIV1UsersRequest{CID: 2}, // Not enough rating to read user
			expectedStatus:   http.StatusForbidden,
			expectedErrorMsg: "must be at least Supervisor to read users",
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingS1, protocol.PilotRatingPPL, []string{"dashboard"}), // Not a Supervisor
			},
		},
		{
			name:             "Forbidden User Creation of administrator by supervisor",
			method:           "POST",
			authorization:    "Bearer valid_token",
			requestBody:      web.APIV1UsersRequest{User: database.FSDUserRecord{CID: 322, NetworkRating: 12}}, // Not enough rating to create user
			expectedStatus:   http.StatusForbidden,
			expectedErrorMsg: "created user must be below supervisor rating",
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingSUP, protocol.PilotRatingPPL, []string{"dashboard"}),
			},
		},
		{
			name:           "Forbidden User Delete - lower than supervisor",
			method:         "DELETE",
			authorization:  "Bearer valid_token",
			requestBody:    web.APIV1UsersRequest{CID: demoRecord.CID},
			expectedStatus: http.StatusForbidden,
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingI3, protocol.PilotRatingPPL, []string{"dashboard"}),
			},
		},
		{
			name:           "Successful User Delete",
			method:         "DELETE",
			authorization:  "Bearer valid_token",
			requestBody:    web.APIV1UsersRequest{CID: demoRecord.CID},
			expectedStatus: http.StatusOK,
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingADM, protocol.PilotRatingPPL, []string{"dashboard"}),
			},
		},
		{
			name:           "invalid audience",
			method:         "DELETE",
			authorization:  "Bearer valid_token",
			requestBody:    web.APIV1UsersRequest{CID: demoRecord.CID},
			expectedStatus: http.StatusForbidden,
			verifier: &MockVerifier{
				Claims: auth.NewFSDJWTClaims(1, protocol.NetworkRatingADM, protocol.PilotRatingPPL, []string{"fsd"}),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := new(bytes.Buffer)
			json.NewEncoder(body).Encode(tt.requestBody)

			var req *http.Request
			if tt.method == "GET" {
				req = httptest.NewRequest(tt.method, "/api/v1/users/"+strconv.Itoa(tt.requestBody.CID), nil)
			} else {
				req = httptest.NewRequest(tt.method, "/api/v1/users", body)
			}

			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}

			rr := httptest.NewRecorder()
			handler := func(w http.ResponseWriter, r *http.Request) {
				web.APIV1UsersHandler(w, r, tt.verifier) // Inject the verifier
			}

			handler(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)

			if rr.Header().Get("Content-Type") != "application/json" {
				t.Fatal("response type not application/json")
			}

			if tt.expectedErrorMsg != "" {
				var resp web.APIV1UsersResponse
				if err := json.NewDecoder(rr.Body).Decode(&resp); err == nil { // Check for decode errors
					assert.Equal(t, tt.expectedErrorMsg, resp.StatusMessage)
				}
			}
		})
	}

	cancelCtx()
	<-b.Done
}
