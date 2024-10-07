package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"io"
	"strconv"
	"time"
)

// FSDJWTRequest represents the vatsim-specific /api/fsd-jwt JSON request payload
type FSDJWTRequest struct {
	CID      string `json:"cid"`
	Password string `json:"password"`
}

// FSDJWTResponse represents the vatsim-specific /api/fsd-jwt JSON response payload
type FSDJWTResponse struct {
	Success  bool   `json:"success"`
	Token    string `json:"token,omitempty"`
	ErrorMsg string `json:"error_msg,omitempty"`
}

// FSDJWTClaims is the standard claims type for openfsd tokens
type FSDJWTClaims struct {
	cid              int
	controllerRating protocol.NetworkRating
	pilotRating      protocol.PilotRating
	audience         jwt.ClaimStrings
}

func (c *FSDJWTClaims) CID() int {
	return c.cid
}

func (c *FSDJWTClaims) ControllerRating() protocol.NetworkRating {
	return c.controllerRating
}

func (c *FSDJWTClaims) PilotRating() protocol.PilotRating {
	return c.pilotRating
}

func (c *FSDJWTClaims) Audience() jwt.ClaimStrings {
	return c.audience
}

func NewFSDJWTClaims(cid int, networkRating protocol.NetworkRating, pilotRating protocol.PilotRating, audience []string) *FSDJWTClaims {
	return &FSDJWTClaims{
		cid:              cid,
		controllerRating: networkRating,
		pilotRating:      pilotRating,
		audience:         audience,
	}
}

type FSDJWTCustomClaims struct {
	jwt.RegisteredClaims
	ControllerRating int `json:"controller_rating"`
	PilotRating      int `json:"pilot_rating"`
}

// Parse extracts the specific FSDJWTClaims out of a previously verified token
func (c *FSDJWTClaims) Parse(token *jwt.Token) (err error) {
	// Parse CID from subject
	var cidStr string
	if cidStr, err = token.Claims.GetSubject(); err != nil {
		return err
	}
	if c.cid, err = strconv.Atoi(cidStr); err != nil {
		return err
	}

	// Parse audience
	if c.audience, err = token.Claims.GetAudience(); err != nil {
		return err
	}

	// Parse vatsim-specific custom claims (controller_rating, pilot_rating)

	mapClaims := token.Claims.(jwt.MapClaims)

	var controllerRatingFloat, pilotRatingFloat float64
	var ok bool
	if controllerRatingFloat, ok = mapClaims["controller_rating"].(float64); !ok {
		return errors.New("controller_rating claim does not exist")
	}
	if pilotRatingFloat, ok = mapClaims["pilot_rating"].(float64); !ok {
		return errors.New("pilot_rating claim does not exist")
	}

	c.controllerRating, c.pilotRating = protocol.NetworkRating(controllerRatingFloat), protocol.PilotRating(pilotRatingFloat)

	return nil
}

// MakeToken makes a JWT token string for the provided claims
func (c *FSDJWTClaims) MakeToken(expiry time.Time) (token string, err error) {
	randomBytes := make([]byte, 16)
	if _, err = io.ReadFull(rand.Reader, randomBytes); err != nil {
		return "", err
	}
	id := base64.StdEncoding.EncodeToString(randomBytes)

	now := time.Now()

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, FSDJWTCustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "openfsd",
			Subject:   strconv.Itoa(c.cid),
			Audience:  c.audience,
			ExpiresAt: jwt.NewNumericDate(expiry),
			NotBefore: jwt.NewNumericDate(now.Add(-120 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        id,
		},
		ControllerRating: int(c.controllerRating),
		PilotRating:      int(c.pilotRating),
	})

	if token, err = t.SignedString(servercontext.JWTKey()); err != nil {
		return "", err
	}

	return
}
