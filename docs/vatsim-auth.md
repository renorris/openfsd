# VATSIM Auth

## Overview

VATSIM employs a bidirectional obfuscation scheme to "verify" a client's authenticity over an FSD connection.

Every VATSIM-approved client receives a unique unsigned 16-bit integer client ID and a 32-byte private key.

There are two parties in an FSD connection: the client, and the server.
When an FSD connection is established, the client and the server each send some random data in the [Client Identification](/protocol#client-identification-di) and [Server Identification](/protocol#server-identification-di) packets, respectively.

Using all of the above values, two distinguishable Auth States are constructed, one for the server, and one for the client.

## Auth State
An Auth State can be described as such:
```go
type AuthState struct {
	clientID  uint16 // Assigned Client ID
	initState string // Initial State
	currState string // Current State
}
```

## Auth State Construction

To construct the initial Auth State, (once the [Client Identification](/protocol#client-identification-di) and [Server Identification](/protocol#server-identification-di) packets have been exchanged)
the following operations are ran:

1. Set the Client ID using the static assigned value for our client. The same value is used for both the server and client states.
2. Set the Current State to the client's assigned private key. The same value is used for both the server and client states.
3. The server and the client each use the random data they _received_ from the _other_ side of the connection to each run their own round of the 'Obfuscation Scheme'. The random data is passed into the scheme as the "challenge" string.
4. Set the Initial State **and** the Current State to the result of this 'Obfuscation Scheme' round.

## Obfuscation Scheme

The 'Obfuscation Scheme' is described as follows:

Inputs: AuthState, and a "challenge" string.<br>
Output: 32-byte **hexadecimal-encoded** MD5 hash.

```go
func (state *AuthState) ObfuscationScheme(challenge string) string {
	
	// Split the challenge into two halves
	c1, c2 := challenge[:(len(challenge)/2)], challenge[(len(challenge)/2):]

	// If the Client ID is an odd number, swap the two halves.
	if (state.clientID & 1) == 1 {
		c1, c2 = c2, c1
	}

	// Split the current state into three parts
	s1, s2, s3 := state.currState[0:12], state.currState[12:22], state.currState[22:32]

	// Declare a temporary buffer
	var h string
	
	// Interleave the above values
	switch state.clientID % 3 {
	case 0:
		h = s1 + c1 + s2 + c2 + s3
	case 1:
		h = s2 + c1 + s3 + c2 + s1
	default:
		h = s3 + c1 + s1 + c2 + s2
	}

	// Generate an MD5 sum from the temporary buffer's value.
	hash := md5.Sum([]byte(h.String()))
	
	// Return a 32-byte hexadecimal representation of the hash.
	return hex.EncodeToString(hash[:])
}
```

## Interrogations

Once the initial Auth States have been constructed, both sides of the connection are able to interrogate (or "challenge") the other using [Auth Challenge](/protocol#auth-challenge-zc) and [Auth Response](/protocol#auth-response-zr) packets.

The mechanism is as follows:

1. An [Auth Challenge](/protocol#auth-challenge-zc) contains a random hexadecimal-encoded bytearray.
2. Along with the current Auth State, this challenge value is fed into a round of Obfuscation Scheme.
3. Send the scheme's return value to the other side of the connection using an [Auth Response](/protocol#auth-response-zr) packet.
4. Concatenate the return value of this round (a 32-byte hexadecimal-encoded byte array) onto the Initial State (initState + returnValue) resulting in a 64-byte hexadecimal encoded array.
5. Generate an MD5 sum using this 64-byte value as input.
6. Set the Current State to the _hexadecimal-representation_ of this hash sum. This value is used to compute the next round when the next [Auth Challenge](/protocol#auth-challenge-zc) packet is received.

Keep in mind: the receiver of the [Auth Response](/protocol#auth-response-zr) packet must maintain a "mirror" version of the other side of the connection's Auth State in order to verify their responses.

## Known Clients

A list of known clients is as follows:

| Client ID | Private Key                        | Client Name |
|-----------|------------------------------------|-------------|
| `8464`    | `945507c4c50222c34687e742729252e6` | vSTARS      |
| `10452`   | `0ad74157c7f449c216bfed04f3af9fb9` | vERAM       |
| `24515`   | `3424cbcebcca6fe95f973b350ff85cef` | vatSys      |
| `27095`   | `3518a62c421937ffa46ac3316957da43` | Euroscope   |
| `33456`   | `52d9343020e9c7d0c6b04b0cca20ad3b` | swift       |
| `35044`   | `fe28334fb753cf0e3d19942197b9ce3e` | vPilot      |
| `55538`   | `ImuL1WbbhVuD8d3MuKpWn2rrLZRa9iVP` | xPilot      |
| `56862`   | `3518a62c421937ffa46ac3316957da43` | VRC         |
