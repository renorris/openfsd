package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

// FSDError combines an FSD error with a golang error, so they can be used interchangeably

type ErrorCode int

const (
	OkError = iota
	CallsignInUseError
	CallsignInvalidError
	AlreadyRegisteredError
	SyntaxError
	PDUSourceInvalidError
	InvalidLogonError
	NoSuchCallsignError
	NoFlightPlanError
	NoWeatherProfileError
	InvalidProtocolRevisionError
	RequestedLevelTooHighError
	ServerFullError
	CertificateSuspendedError
	InvalidControlError
	InvalidPositionForRatingError
	UnauthorizedSoftwareError
	ClientAuthenticationResponseTimeoutError
)

var genericErrorMessage = map[ErrorCode]string{
	OkError:                                  "OK",
	CallsignInUseError:                       "Callsign already in use",
	CallsignInvalidError:                     "Invalid callsign",
	AlreadyRegisteredError:                   "Already registered",
	SyntaxError:                              "Syntax error",
	PDUSourceInvalidError:                    "PDU source invalid",
	InvalidLogonError:                        "Invalid login information",
	NoSuchCallsignError:                      "No such callsign",
	NoFlightPlanError:                        "No flight plan filed",
	NoWeatherProfileError:                    "No weather profile",
	InvalidProtocolRevisionError:             "Invalid protocol revision",
	RequestedLevelTooHighError:               "Requested level too high",
	ServerFullError:                          "Server full",
	CertificateSuspendedError:                "Certificate suspended",
	InvalidControlError:                      "Invalid control",
	InvalidPositionForRatingError:            "Invalid position for rating",
	UnauthorizedSoftwareError:                "Unauthorized software",
	ClientAuthenticationResponseTimeoutError: "Client authentication response timeout",
}

type FSDError struct {
	From    string
	To      string
	Code    ErrorCode
	Param   string
	Message string
}

func (e *FSDError) Error() string {
	return genericErrorMessage[e.Code]
}

func (e *FSDError) Serialize() string {
	return fmt.Sprintf("$ER%s:%s:%03d:%s:%s%s", e.From, e.To, e.Code, e.Param, e.Message, PacketDelimeter)
}

func ParseNetworkErrorPDU(rawPacket string) (*FSDError, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$ER")
	fields := strings.SplitN(rawPacket, Delimeter, 5)
	if len(fields) < 5 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	errCode, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &FSDError{
		From:    fields[0],
		To:      fields[1],
		Code:    ErrorCode(errCode),
		Param:   fields[3],
		Message: fields[4],
	}, nil
}

func NewGenericFSDError(code ErrorCode) *FSDError {
	return &FSDError{
		From:    "server",
		To:      "unknown",
		Code:    code,
		Param:   "",
		Message: genericErrorMessage[code],
	}
}
