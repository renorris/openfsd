package protocol

// PacketType identifies an FSD wire packet kind.
type PacketType int

const (
	PacketTypeUnknown PacketType = iota
	PacketTypeTextMessage
	PacketTypePilotPosition
	PacketTypePilotPositionFast
	PacketTypePilotPositionSlow
	PacketTypePilotPositionStopped
	PacketTypeATCPosition
	PacketTypeSecondaryVisCenter // ' CALLSIGN:INDEX:LAT:LON (SECPOS)
	PacketTypeDeleteATC
	PacketTypeDeletePilot
	PacketTypeClientQuery
	PacketTypeClientQueryResponse
	PacketTypeProController
	PacketTypeSquawkbox
	PacketTypeMetarRequest
	PacketTypeKillRequest
	PacketTypeAuthChallenge
	PacketTypeHandoffRequest
	PacketTypeHandoffAccept
	PacketTypeFlightPlan
	PacketTypeFlightPlanAmendment

	// Login / identification / error types (not handled by fsd post-login verifyPacket).
	PacketTypeServerIdent
	PacketTypeClientIdent
	PacketTypeAddPilot
	PacketTypeAddATC
	PacketTypeError
)

// ErrorCode is an FSD server error code (wire values 1–17).
type ErrorCode int

const (
	CallsignInUseError                       ErrorCode = 1  // Callsign is already in use
	CallsignInvalidError                     ErrorCode = 2  // Callsign is invalid
	AlreadyRegisteredError                   ErrorCode = 3  // Client is already registered
	SyntaxError                              ErrorCode = 4  // Packet syntax is invalid
	SourceInvalidError                       ErrorCode = 5  // Packet source is invalid
	InvalidLogonError                        ErrorCode = 6  // Login credentials or token are invalid
	NoSuchCallsignError                      ErrorCode = 7  // Specified callsign does not exist
	NoFlightPlanError                        ErrorCode = 8  // No flight plan found for the Client
	NoWeatherProfileError                    ErrorCode = 9  // No weather profile available
	InvalidProtocolRevisionError             ErrorCode = 10 // Client uses an unsupported protocol version
	RequestedLevelTooHighError               ErrorCode = 11 // Requested access level is too high
	ServerFullError                          ErrorCode = 12 // Server has reached capacity
	CertificateSuspendedError                ErrorCode = 13 // Client's certificate is suspended
	InvalidControlError                      ErrorCode = 14 // Invalid control command
	InvalidPositionForRatingError            ErrorCode = 15 // Position not allowed for Client's rating
	UnauthorizedSoftwareError                ErrorCode = 16 // Client software is not authorized
	ClientAuthenticationResponseTimeoutError ErrorCode = 17 // Authentication response timed out
)

// NetworkRating is a VATSIM network rating protocol value.
// NetworkRatingAdministator keeps the historical misspelling for wire compatibility.
type NetworkRating int

const (
	NetworkRatingInactive NetworkRating = iota - 1
	NetworkRatingSuspended
	NetworkRatingObserver
	NetworkRatingStudent1
	NetworkRatingStudent2
	NetworkRatingStudent3
	NetworkRatingController1
	NetworkRatingController2
	NetworkRatingController3
	NetworkRatingInstructor1
	NetworkRatingInstructor2
	NetworkRatingInstructor3
	NetworkRatingSupervisor
	NetworkRatingAdministator
)
