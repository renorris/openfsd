package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"net/http"
	"os"
	"testing"
	"time"
)

func doJwtRequest(t *testing.T, url string, cid int, password string) *JwtResponse {
	jwtRequest := JwtRequest{
		CID:      fmt.Sprintf("%d", cid),
		Password: password,
	}
	jsonData, err := json.Marshal(jwtRequest)
	assert.Nil(t, err)

	client := http.Client{}
	resp, err := client.Post(url, "application/json", bytes.NewReader(jsonData))
	assert.Nil(t, err)

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	assert.Nil(t, err)
	var jwtResponse JwtResponse
	err = json.Unmarshal(buf.Bytes(), &jwtResponse)
	assert.Nil(t, err)

	return &jwtResponse
}

func TestServeJwtAuthTokens(t *testing.T) {
	SC = &ServerConfig{
		FsdListenAddr:  "localhost:6809",
		HttpListenAddr: "localhost:9086",
		HttpsEnabled:   false,
		DatabaseFile:   "./test.db",
		MOTD:           "",
	}
	os.Remove(SC.DatabaseFile)
	defer os.Remove(SC.DatabaseFile)

	configureDatabase()
	configureJwt()
	configurePostOffice()

	addUserToDatabase(t, 1000000, "12345", 1)

	// Start http server
	httpCtx, cancelHttp := context.WithCancel(context.Background())
	go StartHttpServer(httpCtx)
	defer cancelHttp()
	time.Sleep(50 * time.Millisecond)

	// Test successful request
	{
		jwtResponse := doJwtRequest(t, "http://localhost:9086/api/fsd-jwt", 1000000, "12345")

		assert.True(t, jwtResponse.Success)
		assert.NotEmpty(t, jwtResponse.Token)
		assert.Empty(t, jwtResponse.ErrorMsg)

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(jwtResponse.Token, &claims, func(token *jwt.Token) (interface{}, error) {
			return JWTKey, nil
		})
		assert.Nil(t, err)
		assert.NotNil(t, claims)
		exp, err := token.Claims.GetExpirationTime()
		assert.Nil(t, err)
		iat, err := token.Claims.GetIssuedAt()
		assert.Nil(t, err)
		assert.True(t, exp.Sub(iat.Time) == 420*time.Second)
	}

	// Test invalid CID
	{
		jwtResponse := doJwtRequest(t, "http://localhost:9086/api/fsd-jwt", 9999999, "12345")

		assert.False(t, jwtResponse.Success)
		assert.Empty(t, jwtResponse.Token)
		assert.Equal(t, jwtResponse.ErrorMsg, "User not found")
	}

	// Test invalid password
	{
		jwtResponse := doJwtRequest(t, "http://localhost:9086/api/fsd-jwt", 1000000, "54321")

		assert.False(t, jwtResponse.Success)
		assert.Empty(t, jwtResponse.Token)
		assert.Equal(t, jwtResponse.ErrorMsg, "Password is incorrect")
	}
}
