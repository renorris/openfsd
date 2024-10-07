package protocol

import (
	"errors"
	"fmt"
	"github.com/go-playground/validator/v10"
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
	CallsignInUseError:                       "callsign already in use",
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
	return e.Serialize()
}

func (e *FSDError) Serialize() string {
	return fmt.Sprintf("$ER%s:%s:%03d:%s:%s%s", e.From, e.To, e.Code, e.Param, e.Message, PacketDelimiter)
}

func ParseNetworkErrorPDU(rawPacket string) (*FSDError, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimiter)
	rawPacket = strings.TrimPrefix(rawPacket, "$ER")
	fields := strings.SplitN(rawPacket, Delimiter, 5)
	if len(fields) < 5 {
		return nil, NewGenericFSDError(SyntaxError, "", "invalid field count")
	}

	errCode, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError, fields[2], "invalid error code")
	}

	return &FSDError{
		From:    fields[0],
		To:      fields[1],
		Code:    ErrorCode(errCode),
		Param:   fields[3],
		Message: fields[4],
	}, nil
}

func NewGenericFSDError(code ErrorCode, param string, messageContext string) *FSDError {
	msg := genericErrorMessage[code]
	if messageContext != "" {
		msg += fmt.Sprintf(": %s", messageContext)
	}

	return &FSDError{
		From:    "server",
		To:      "unknown",
		Code:    code,
		Param:   param,
		Message: msg,
	}
}

func getFSDErrorFromValidatorErrors(err error) *FSDError {
	var validationErrors validator.ValidationErrors
	if errors.As(err, &validationErrors) {
		if len(validationErrors) < 1 {
			return nil
		}
		return NewGenericFSDError(SyntaxError, "", "validation error: "+validationErrors[0].Error())
	}

	return nil
}
