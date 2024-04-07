package vatsimauth

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
)

var Keys = map[uint16]string{
	8464:  "945507c4c50222c34687e742729252e6",
	10452: "0ad74157c7f449c216bfed04f3af9fb9",
	27095: "3518a62c421937ffa46ac3316957da43",
	33456: "52d9343020e9c7d0c6b04b0cca20ad3b",
	35044: "fe28334fb753cf0e3d19942197b9ce3e",
	55538: "ImuL1WbbhVuD8d3MuKpWn2rrLZRa9iVP",
	56862: "3518a62c421937ffa46ac3316957da43",
}

type VatsimAuth struct {
	clientID uint16
	init     string
	state    string
}

func NewVatsimAuth(clientID uint16, privateKey string) *VatsimAuth {
	return &VatsimAuth{
		clientID: clientID,
		init:     "",
		state:    privateKey,
	}
}

// SetInitialChallenge stores the initial challenge for this authentication state. This should be called once before running GenerateResponse
func (v *VatsimAuth) SetInitialChallenge(initialChallenge string) {
	v.init = v.GenerateResponse(initialChallenge)
	v.state = v.init
}

// GenerateResponse returns the response for the provided challenge.
func (v *VatsimAuth) GenerateResponse(challenge string) string {
	c1, c2 := challenge[0:(len(challenge)/2)], challenge[(len(challenge)/2):]

	if (v.clientID & 1) == 1 {
		c1, c2 = c2, c1
	}

	s1, s2, s3 := v.state[0:12], v.state[12:22], v.state[22:32]

	h := ""
	switch v.clientID % 3 {
	case 0:
		h = s1 + c1 + s2 + c2 + s3
	case 1:
		h = s2 + c1 + s3 + c2 + s1
	default:
		h = s3 + c1 + s1 + c2 + s2
	}

	hash := md5.Sum([]byte(h))
	return hex.EncodeToString(hash[:])
}

// UpdateState updates this authentication state with a response hash. This is conventionally called with the return value of GenerateResponse
func (v *VatsimAuth) UpdateState(hash string) {
	newStateHash := md5.Sum([]byte(v.init + hash))
	v.state = hex.EncodeToString(newStateHash[:])
}

// GenerateChallenge returns a cryptographically secure random challenge string
func GenerateChallenge() (string, error) {
	challenge := make([]byte, 4)
	_, err := rand.Read(challenge)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(challenge), nil
}
