package server

import (
	"bytes"
	"strconv"
	"time"

	"github.com/renorris/openfsd/internal/session"
)

// parseLoginPackets parses client ident + add packets into LoginData and password/JWT token.
// Does not perform I/O. errCode/errMsg are set when err != nil for wire $ER responses.
func parseLoginPackets(idPacket, addPacket []byte, now time.Time) (data session.LoginData, token string, errCode int, errMsg string, err error) {
	// Check if the client sent a challenge field
	if countFields(idPacket) == 9 {
		data.ClientChallenge = string(getField(idPacket, 8))

		var clientId uint64
		clientId, err = strconv.ParseUint(string(getField(idPacket, 2)), 16, 16)
		if err != nil {
			err = ErrInvalidIDPacket
			errCode = SyntaxError
			errMsg = "Error parsing client ID"
			return
		}
		data.ClientID = uint16(clientId)
	}

	if len(addPacket) < 16 {
		err = ErrInvalidAddPacket
		errCode = SyntaxError
		errMsg = "Invalid add packet"
		return
	}

	var prefix string
	switch string(addPacket[:3]) {
	case "#AA":
		data.IsAtc = true
		prefix = "#AA"
	case "#AP":
		prefix = "#AP"
	default:
		err = ErrInvalidAddPacket
		errCode = SyntaxError
		errMsg = "Invalid add packet prefix"
		return
	}

	if data.IsAtc {
		if countFields(addPacket) != 7 {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid number of fields in ATC add packet"
			return
		}
	} else {
		if countFields(addPacket) != 8 {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid number of fields in pilot add packet"
			return
		}
	}

	if callsign, found := bytes.CutPrefix(getField(addPacket, 0), []byte(prefix)); found {
		data.Callsign = string(callsign)
	} else {
		err = ErrInvalidAddPacket
		errCode = SyntaxError
		errMsg = "Invalid callsign in add packet"
		return
	}

	if data.IsAtc {
		data.RealName = string(getField(addPacket, 2))
		if data.CID, err = strconv.Atoi(string(getField(addPacket, 3))); err != nil {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid CID in ATC add packet"
			return
		}
		token = string(getField(addPacket, 4))
		var networkRating int
		if networkRating, err = strconv.Atoi(string(getField(addPacket, 5))); err != nil {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid network rating in pilot add packet"
			return
		}
		data.NetworkRating = NetworkRating(networkRating)
		if data.ProtoRevision, err = strconv.Atoi(string(getField(addPacket, 6))); err != nil {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid protocol revision in ATC add packet"
			return
		}
	} else {
		if data.CID, err = strconv.Atoi(string(getField(addPacket, 2))); err != nil {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid CID in pilot add packet"
			return
		}
		token = string(getField(addPacket, 3))
		var networkRating int
		if networkRating, err = strconv.Atoi(string(getField(addPacket, 4))); err != nil {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid network rating in pilot add packet"
			return
		}
		data.NetworkRating = NetworkRating(networkRating)
		if data.ProtoRevision, err = strconv.Atoi(string(getField(addPacket, 5))); err != nil {
			err = ErrInvalidAddPacket
			errCode = SyntaxError
			errMsg = "Invalid protocol revision in pilot add packet"
			return
		}
		data.RealName = string(getField(addPacket, 7))
	}

	if data.ProtoRevision < 100 || data.ProtoRevision > 101 {
		err = ErrInvalidAddPacket
		errCode = InvalidProtocolRevisionError
		errMsg = "Invalid protocol revision"
		return
	}

	data.LoginTime = now
	return
}
