package fsdclient

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"strings"
)

// Challenge helpers for $ZC / $ZR without importing internal/auth or fsd.
//
// Two options are provided:
//  1. Precomputed responses via FormatAuthResponse / SendAuthResponse.
//  2. A minimal client-side VATSIM-auth reimplementation (ChallengeState)
//     for tests with known client keys (duplicated as fixtures, not imported).

// ErrUnsupportedClient is returned when ChallengeState has no key for a client ID.
var ErrUnsupportedClient = errors.New("fsdclient: unsupported auth client id")

// FormatAuthChallenge builds a $ZC packet: $ZC{from}:{to}:{challenge}\r\n
func FormatAuthChallenge(from, to, challenge string) []byte {
	var b strings.Builder
	b.Grow(8 + len(from) + len(to) + len(challenge))
	b.WriteString("$ZC")
	b.WriteString(from)
	b.WriteByte(':')
	b.WriteString(to)
	b.WriteByte(':')
	b.WriteString(challenge)
	b.WriteString("\r\n")
	return []byte(b.String())
}

// FormatAuthResponse builds a $ZR packet: $ZR{from}:{to}:{response}\r\n
func FormatAuthResponse(from, to, response string) []byte {
	var b strings.Builder
	b.Grow(8 + len(from) + len(to) + len(response))
	b.WriteString("$ZR")
	b.WriteString(from)
	b.WriteByte(':')
	b.WriteString(to)
	b.WriteByte(':')
	b.WriteString(response)
	b.WriteString("\r\n")
	return []byte(b.String())
}

// SendAuthChallenge sends a $ZC packet with the given challenge string.
func (c *Client) SendAuthChallenge(from, to, challenge string) error {
	return c.Send(FormatAuthChallenge(from, to, challenge))
}

// SendAuthResponse sends a $ZR packet with a precomputed response.
// Use this when the response is computed outside the client (hooks).
func (c *Client) SendAuthResponse(from, to, response string) error {
	return c.Send(FormatAuthResponse(from, to, response))
}

// KnownAuthKeys are well-known VATSIM client software keys used for tests and
// local challenge helpers. Duplicated byte-for-byte from fsd/vatsimauth.go so
// pkg/fsdclient does not import fsd or internal/auth (package boundary).
// Keep in sync when server keys change; dual-maintenance is intentional.
var KnownAuthKeys = map[uint16]string{
	8464:  "945507c4c50222c34687e742729252e6", // vSTARS
	10452: "0ad74157c7f449c216bfed04f3af9fb9", // vERAM
	24515: "3424cbcebcca6fe95f973b350ff85cef", // vatSys
	27095: "3518a62c421937ffa46ac3316957da43", // Euroscope
	33456: "52d9343020e9c7d0c6b04b0cca20ad3b", // swift
	35044: "fe28334fb753cf0e3d19942197b9ce3e", // vPilot
	48312: "bc2eb1ef4d96709c683084055dd5e83f", // TWRTrainer
	55538: "ImuL1WbbhVuD8d3MuKpWn2rrLZRa9iVP", // xPilot
	56862: "3518a62c421937ffa46ac3316957da43", // VRC
}

// ChallengeState is a minimal client-side VATSIM-auth state machine for
// answering $ZC challenges in tests (or when the key is known).
type ChallengeState struct {
	init     [16]byte
	curr     [16]byte
	clientID uint16
}

// Initialize seeds the state from a client software ID and the initial
// challenge key (typically from $ID field 8 / server $DI field 3).
// challenge should be the raw challenge string bytes (hex ascii), matching
// openfsd server behaviour.
func (s *ChallengeState) Initialize(clientID uint16, initialChallenge []byte) error {
	keyStr, ok := KnownAuthKeys[clientID]
	if !ok {
		return ErrUnsupportedClient
	}
	s.clientID = clientID
	var key [32]byte
	copy(key[:], keyStr)
	s.init = s.runObfuscationRound(&key, initialChallenge)
	s.curr = s.init
	return nil
}

// ResponseForChallenge returns the 32-char hex response for a server $ZC challenge.
func (s *ChallengeState) ResponseForChallenge(challenge []byte) [32]byte {
	curr := s.currAsHex()
	round := s.runObfuscationRound(&curr, challenge)
	var res [32]byte
	hex.Encode(res[:], round[:])
	return res
}

// UpdateState advances the running state after a challenge/response exchange
// (d is the 32-byte hex-encoded response, matching openfsd server).
func (s *ChallengeState) UpdateState(d *[32]byte) {
	init := s.initAsHex()
	var tmp [64]byte
	copy(tmp[:32], init[:])
	copy(tmp[32:], d[:])
	s.curr = md5.Sum(tmp[:])
}

// SendChallengeResponse computes a response with s and sends $ZR.
func (c *Client) SendChallengeResponse(s *ChallengeState, from, to string, challenge []byte) error {
	res := s.ResponseForChallenge(challenge)
	if err := c.SendAuthResponse(from, to, string(res[:])); err != nil {
		return err
	}
	s.UpdateState(&res)
	return nil
}

func (s *ChallengeState) initAsHex() (d [32]byte) {
	hex.Encode(d[:], s.init[:])
	return
}

func (s *ChallengeState) currAsHex() (d [32]byte) {
	hex.Encode(d[:], s.curr[:])
	return
}

func (s *ChallengeState) runObfuscationRound(curr *[32]byte, challenge []byte) (res [16]byte) {
	c1, c2 := challenge[0:(len(challenge)/2)], challenge[(len(challenge)/2):]
	if (s.clientID & 1) == 1 {
		c1, c2 = c2, c1
	}
	s1, s2, s3 := curr[0:12], curr[12:22], curr[22:32]
	tmp := make([]byte, 0, 64)
	switch s.clientID % 3 {
	case 0:
		tmp = append(tmp, s1...)
		tmp = append(tmp, c1...)
		tmp = append(tmp, s2...)
		tmp = append(tmp, c2...)
		tmp = append(tmp, s3...)
	case 1:
		tmp = append(tmp, s2...)
		tmp = append(tmp, c1...)
		tmp = append(tmp, s3...)
		tmp = append(tmp, c2...)
		tmp = append(tmp, s1...)
	default:
		tmp = append(tmp, s3...)
		tmp = append(tmp, c1...)
		tmp = append(tmp, s1...)
		tmp = append(tmp, c2...)
		tmp = append(tmp, s2...)
	}
	return md5.Sum(tmp)
}
