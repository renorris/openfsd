package protocol

import "github.com/go-playground/validator/v10"

var V *validator.Validate

// Misc FSD protocol values
const (
	ClientQueryBroadcastRecipient       = "@94835"
	ClientQueryBroadcastRecipientPilots = "@94386"
	Delimiter                           = ":"
	PacketDelimiter                     = "\r\n"
	ServerCallsign                      = "SERVER"
)

const (
	NetworkFacilityOBS = iota
	NetworkFacilityFSS
	NetworkFacilityDEL
	NetworkFacilityGND
	NetworkFacilityTWR
	NetworkFacilityAPP
	NetworkFacilityCTR
)

const (
	SimulatorTypeUnknown = iota
	SimulatorTypeMSFS95
	SimulatorTypeMSFS98
	SimulatorTypeMSCFS
	SimulatorTypeMSFS2000
	SimulatorTypeMSCFS2
	SimulatorTypeMSFS2002
	SimulatorTypeMSCFS3
	SimulatorTypeMSFS2004
	SimulatorTypeMSFSX
	SimulatorTypeXPlane8 = iota + 2
	SimulatorTypeXPlane9
	SimulatorTypeXPlane10
	SimulatorTypeXPlane11   = iota + 3
	SimulatorTypeFlightGear = iota + 11
	SimulatorTypeP3D        = iota + 15
)

const (
	ProtoRevisionUnknown      = 0
	ProtoRevisionClassic      = 9
	ProtoRevisionVatsimNoAuth = 10
	ProtoRevisionVatsimAuth   = 100
	ProtoRevisionVatsim2022   = 101
)

const (
	ClientQueryUnknown               = ""
	ClientQueryIsValidATC            = "ATC"
	ClientQueryCapabilities          = "CAPS"
	ClientQueryCOM1Freq              = "C?"
	ClientQueryRealName              = "RN"
	ClientQueryServer                = "SV"
	ClientQueryATIS                  = "ATIS"
	ClientQueryPublicIP              = "IP"
	ClientQueryINF                   = "INF"
	ClientQueryFlightPlan            = "FP"
	ClientQueryIPC                   = "IPC"
	ClientQueryRequestRelief         = "BY"
	ClientQueryCancelRequestRelief   = "HI"
	ClientQueryRequestHelp           = "HLP"
	ClientQueryCancelRequestHelp     = "NOHLP"
	ClientQueryWhoHas                = "WH"
	ClientQueryInitiateTrack         = "IT"
	ClientQueryAcceptHandoff         = "HT"
	ClientQueryDropTrack             = "DR"
	ClientQuerySetFinalAltitude      = "FA"
	ClientQuerySetTempAltitude       = "TA"
	ClientQuerySetBeaconCode         = "BC"
	ClientQuerySetScratchpad         = "SC"
	ClientQuerySetVoiceType          = "VT"
	ClientQueryAircraftConfiguration = "ACC"
	ClientQueryNewInfo               = "NEWINFO"
	ClientQueryNewATIS               = "NEWATIS"
	ClientQueryEstimate              = "EST"
	ClientQuerySetGlobalData         = "GD"
)

type PDU interface {
	Parse(string) error
	Serialize() string
}
