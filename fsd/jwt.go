package fsd

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"time"
)

const issuer = "openfsd"

type JwtToken struct {
	*jwt.Token
}

type CustomClaims struct {
	jwt.RegisteredClaims
	CustomFields
}

type CustomFields struct {
	TokenType     string        `json:"token_type"`
	CID           int           `json:"cid"`
	FirstName     string        `json:"first_name,omitempty"`
	LastName      string        `json:"last_name,omitempty"`
	NetworkRating NetworkRating `json:"network_rating"`
}

func (t *JwtToken) CustomClaims() *CustomClaims {
	return t.Claims.(*CustomClaims)
}

func MakeJwtToken(customFields *CustomFields, validityDuration time.Duration) (token *jwt.Token, err error) {
	// Generate random ID
	id, err := uuid.NewRandom()
	if err != nil {
		return
	}

	now := time.Now()
	claims := &CustomClaims{
		jwt.RegisteredClaims{
			Issuer:    issuer,
			ExpiresAt: &jwt.NumericDate{Time: now.Add(validityDuration)},
			NotBefore: &jwt.NumericDate{Time: now.Add(-30 * time.Second)},
			IssuedAt:  &jwt.NumericDate{Time: now},
			ID:        id.String(),
		},
		*customFields,
	}

	token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return
}

func ParseJwtToken(rawToken string, secretKey []byte) (token *JwtToken, err error) {
	var customClaims CustomClaims
	jwtToken, err := jwt.ParseWithClaims(
		rawToken,
		&customClaims, func(_ *jwt.Token) (any, error) {
			return secretKey, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(issuer),
	)
	if err != nil {
		return
	}

	token = &JwtToken{jwtToken}

	return
}
