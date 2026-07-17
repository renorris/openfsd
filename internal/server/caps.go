package server

// serverCapabilitiesWire is the CAPS payload openfsd advertises when a client
// queries $CQ{callsign}:SERVER:CAPS.
//
// Only flags with empirically implemented server behavior are included.
// SECPOS is intentionally omitted until secondary visibility centers land
// (advertising it would cause clients such as vatSys to emit secondary centers
// the server cannot honor).
//
// See docs/capabilities.md for flag inventory, vatSys compatibility gates, and
// justification for each advertised token.
const serverCapabilitiesWire = "VERSION=1:ATCINFO=1:NEWATIS=1:GLOBALDATA=1:ICAOEQ=1:ATCMULTI=1:FASTPOS=1"

// ServerCapabilitiesPayload returns the colon-joined NAME=1 list advertised in
// $CRSERVER:{callsign}:CAPS:… responses. Exported for tests.
func ServerCapabilitiesPayload() string {
	return serverCapabilitiesWire
}
