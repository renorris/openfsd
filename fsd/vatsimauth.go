package fsd

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
)

var ErrUnsupportedAuthClient = errors.New("vatsimauth: unsupported client")

var vatsimAuthKeys = map[uint16]string{
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

type vatsimAuthState struct {
	init, curr [16]byte
	clientId   uint16
}

func (s *vatsimAuthState) initAsHex() (d [32]byte) {
	hex.Encode(d[:], s.init[:])
	return
}

func (s *vatsimAuthState) currAsHex() (d [32]byte) {
	hex.Encode(d[:], s.curr[:])
	return
}

func (s *vatsimAuthState) Initialize(clientId uint16, initialChallenge []byte) (err error) {
	keyStr, ok := vatsimAuthKeys[clientId]
	if !ok {
		err = ErrUnsupportedAuthClient
		return
	}
	s.clientId = clientId

	key := [32]byte{}
	copy(key[:], keyStr)

	s.init = s.runObfuscationRound(&key, initialChallenge)
	s.curr = s.init
	return
}

func (s *vatsimAuthState) IsInitialized() bool {
	return s.clientId != 0
}

func (s *vatsimAuthState) GetResponseForChallenge(challenge []byte) (res [32]byte) {
	curr := s.currAsHex()
	round := s.runObfuscationRound(&curr, challenge)
	hex.Encode(res[:], round[:])
	return
}

func (s *vatsimAuthState) UpdateState(d *[32]byte) {
	init := s.initAsHex()
	tmp := [64]byte{}
	copy(tmp[:32], init[:])
	copy(tmp[32:], d[:])

	s.curr = md5.Sum(tmp[:])
}

func (s *vatsimAuthState) runObfuscationRound(curr *[32]byte, challenge []byte) (res [16]byte) {
	c1, c2 := challenge[0:(len(challenge)/2)], challenge[(len(challenge)/2):]

	if (s.clientId & 1) == 1 {
		c1, c2 = c2, c1
	}

	s1, s2, s3 := curr[0:12], curr[12:22], curr[22:32]

	tmp := make([]byte, 0, 64)
	switch s.clientId % 3 {
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

	res = md5.Sum(tmp)
	return
}
