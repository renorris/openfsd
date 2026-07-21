package server

// serverCapabilitiesWire is the CAPS payload openfsd advertises when a client
// queries $CQ{callsign}:SERVER:CAPS.
//
// Only flags with empirically implemented server behavior are included.
// SECPOS=1 is advertised because secondary visibility centers (' packets) are
// applied to multi-box range search (session.VisBoxesOverlap).
//
// See docs/capabilities.md for flag inventory, vatSys compatibility gates, and
// justification for each advertised token.
const serverCapabilitiesWire = "VERSION=1:SECPOS=1:ATCINFO=1:NEWATIS=1:GLOBALDATA=1:ICAOEQ=1:ATCMULTI=1:FASTPOS=1"

// ServerCapabilitiesPayload returns the colon-joined NAME=1 list advertised in
// $CRSERVER:{callsign}:CAPS:… responses. Exported for tests.
func ServerCapabilitiesPayload() string {
	return serverCapabilitiesWire
}
