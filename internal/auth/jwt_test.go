package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/renorris/openfsd/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeAndParseJwtToken(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt")

	fields := &CustomFields{
		TokenType:     "access",
		CID:           1001001,
		FirstName:     "Test",
		LastName:      "User",
		NetworkRating: protocol.NetworkRatingObserver,
	}

	token, err := MakeJwtToken(fields, 15*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, token)

	signed, err := token.SignedString(secret)
	require.NoError(t, err)
	assert.NotEmpty(t, signed)

	parsed, err := ParseJwtToken(signed, secret)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	claims := parsed.CustomClaims()
	assert.Equal(t, "access", claims.TokenType)
	assert.Equal(t, 1001001, claims.CID)
	assert.Equal(t, "Test", claims.FirstName)
	assert.Equal(t, "User", claims.LastName)
	assert.Equal(t, protocol.NetworkRatingObserver, claims.NetworkRating)
	assert.Equal(t, issuer, claims.Issuer)
	assert.NotEmpty(t, claims.ID)
	assert.NotNil(t, claims.ExpiresAt)
	assert.NotNil(t, claims.NotBefore)
	assert.NotNil(t, claims.IssuedAt)
}

func TestParseJwtTokenBadSignature(t *testing.T) {
	secret := []byte("correct-secret")
	fields := &CustomFields{
		TokenType:     "fsd",
		CID:           1,
		NetworkRating: protocol.NetworkRatingSupervisor,
	}

	token, err := MakeJwtToken(fields, time.Hour)
	require.NoError(t, err)
	signed, err := token.SignedString(secret)
	require.NoError(t, err)

	_, err = ParseJwtToken(signed, []byte("wrong-secret"))
	assert.Error(t, err)
}

func TestParseJwtTokenExpired(t *testing.T) {
	secret := []byte("secret")
	fields := &CustomFields{
		TokenType:     "refresh",
		CID:           42,
		NetworkRating: protocol.NetworkRatingAdministator,
	}

	// Negative validity so ExpiresAt is in the past (NotBefore is now-30s, still valid window for nbf).
	token, err := MakeJwtToken(fields, -time.Hour)
	require.NoError(t, err)
	signed, err := token.SignedString(secret)
	require.NoError(t, err)

	_, err = ParseJwtToken(signed, secret)
	assert.Error(t, err)
}

func TestParseJwtTokenWrongIssuer(t *testing.T) {
	secret := []byte("secret")
	now := time.Now()
	claims := &CustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "not-openfsd",
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "manual-id",
		},
		CustomFields: CustomFields{
			TokenType:     "access",
			CID:           1,
			NetworkRating: protocol.NetworkRatingStudent1,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	require.NoError(t, err)

	_, err = ParseJwtToken(signed, secret)
	assert.Error(t, err)
}

func TestParseJwtTokenMalformed(t *testing.T) {
	_, err := ParseJwtToken("not.a.jwt", []byte("secret"))
	assert.Error(t, err)

	_, err = ParseJwtToken("", []byte("secret"))
	assert.Error(t, err)
}

func TestParseJwtTokenWrongMethod(t *testing.T) {
	secret := []byte("secret")
	now := time.Now()
	claims := &CustomClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "none-alg",
		},
		CustomFields: CustomFields{
			TokenType:     "access",
			CID:           1,
			NetworkRating: protocol.NetworkRatingObserver,
		},
	}
	// Sign with none is rejected by WithValidMethods(["HS256"]).
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = ParseJwtToken(signed, secret)
	assert.Error(t, err)
}

func TestMakeJwtTokenTokenTypes(t *testing.T) {
	secret := []byte("secret")
	for _, tokenType := range []string{"access", "refresh", "fsd", "fsd_service"} {
		fields := &CustomFields{
			TokenType:     tokenType,
			CID:           99,
			NetworkRating: protocol.NetworkRatingController1,
		}
		token, err := MakeJwtToken(fields, 5*time.Minute)
		require.NoError(t, err)
		signed, err := token.SignedString(secret)
		require.NoError(t, err)

		parsed, err := ParseJwtToken(signed, secret)
		require.NoError(t, err)
		assert.Equal(t, tokenType, parsed.CustomClaims().TokenType)
	}
}

func TestCustomFieldsOmitEmptyNames(t *testing.T) {
	secret := []byte("secret")
	fields := &CustomFields{
		TokenType:     "fsd_service",
		CID:           -1,
		NetworkRating: protocol.NetworkRatingAdministator,
	}
	token, err := MakeJwtToken(fields, time.Minute)
	require.NoError(t, err)
	signed, err := token.SignedString(secret)
	require.NoError(t, err)

	parsed, err := ParseJwtToken(signed, secret)
	require.NoError(t, err)
	c := parsed.CustomClaims()
	assert.Empty(t, c.FirstName)
	assert.Empty(t, c.LastName)
	assert.Equal(t, -1, c.CID)
}
