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
	PacketTypeHandoffCancel // $HC — dedicated handoff cancel (parallel to $HO/$HA)
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

// PilotRating is a VATSIM pilot rating wire/API value.
// Values are not a dense 0..N sequence; higher qualifications use increasing IDs
// (historically bitmask-shaped: 0, 1, 3, 7, 15, 31, 63).
// Source: https://vatsim.dev/resources/ratings/ (Pilot table).
type PilotRating int

const (
	PilotRatingNone PilotRating = 0  // P0 — No Pilot Rating
	PilotRatingPPL  PilotRating = 1  // PPL — Private Pilot License
	PilotRatingIR   PilotRating = 3  // IR — Instrument Rating
	PilotRatingCMEL PilotRating = 7  // CMEL — Commercial Multi-Engine License
	PilotRatingATPL PilotRating = 15 // ATPL — Air Transport Pilot License
	PilotRatingFI   PilotRating = 31 // FI — Flight Instructor
	PilotRatingFE   PilotRating = 63 // FE — Flight Examiner
)

// PilotRatingScale is the ordered list of valid pilot ratings (lowest → highest).
// Use this for UI selects and validation; do not assume every integer in range is valid.
var PilotRatingScale = []PilotRating{
	PilotRatingNone,
	PilotRatingPPL,
	PilotRatingIR,
	PilotRatingCMEL,
	PilotRatingATPL,
	PilotRatingFI,
	PilotRatingFE,
}

// IsValidPilotRating reports whether v is one of the official VATSIM pilot rating IDs.
func IsValidPilotRating(v int) bool {
	for _, p := range PilotRatingScale {
		if int(p) == v {
			return true
		}
	}
	return false
}

// PilotRatingShort returns the short code (P0, PPL, IR, …) for a pilot rating ID.
func PilotRatingShort(v int) string {
	switch PilotRating(v) {
	case PilotRatingNone:
		return "P0"
	case PilotRatingPPL:
		return "PPL"
	case PilotRatingIR:
		return "IR"
	case PilotRatingCMEL:
		return "CMEL"
	case PilotRatingATPL:
		return "ATPL"
	case PilotRatingFI:
		return "FI"
	case PilotRatingFE:
		return "FE"
	default:
		return "?"
	}
}

// PilotRatingLong returns the long name for a pilot rating ID.
func PilotRatingLong(v int) string {
	switch PilotRating(v) {
	case PilotRatingNone:
		return "No Pilot Rating"
	case PilotRatingPPL:
		return "Private Pilot License"
	case PilotRatingIR:
		return "Instrument Rating"
	case PilotRatingCMEL:
		return "Commercial Multi-Engine License"
	case PilotRatingATPL:
		return "Air Transport Pilot License"
	case PilotRatingFI:
		return "Flight Instructor"
	case PilotRatingFE:
		return "Flight Examiner"
	default:
		return "Unknown"
	}
}
