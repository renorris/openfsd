package auth

import (
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/servercontext"
	"time"
)

// JWTVerifier is an frontend for verifying JWT tokens
type JWTVerifier interface {
	VerifyJWT(tokenStr string) (*jwt.Token, error)
}

// DefaultVerifier is the default implementation of JWTVerifier
type DefaultVerifier struct{}

// VerifyJWT verifies the signature, issuer, expiration times, and not-before times of a token string
func (d DefaultVerifier) VerifyJWT(tokenStr string) (token *jwt.Token, err error) {
	if token, err = jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		return servercontext.JWTKey(), nil
	}, jwt.WithValidMethods([]string{"HS256"})); err != nil {
		return nil, err
	}

	var issuer string
	if issuer, err = token.Claims.GetIssuer(); err != nil {
		return nil, err
	}
	if issuer != "openfsd" {
		return nil, errors.New("issuer != openfsd")
	}

	// Verify expiration time
	var expirationTime *jwt.NumericDate
	if expirationTime, err = token.Claims.GetExpirationTime(); err != nil {
		return nil, err
	}

	if expirationTime.Before(time.Now()) {
		return nil, errors.New("token expired")
	}

	// Verify not-before time
	var notBeforeTime *jwt.NumericDate
	if notBeforeTime, err = token.Claims.GetNotBefore(); err != nil {
		return nil, err
	}

	if notBeforeTime.After(time.Now()) {
		return nil, errors.New("token not yet valid")
	}

	return
}
