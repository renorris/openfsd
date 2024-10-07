package vatsimauth

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestVatsimAuth(t *testing.T) {

	// vPilot clientID and key
	v := NewVatsimAuth(35044, Keys[35044])
	assert.NotNil(t, v)

	// Initial challenge
	// $DISERVER:CLIENT:VATSIM FSD V3.43:30984979d8caed23
	v.SetInitialChallenge("30984979d8caed23")

	// First challenge from server
	// $ZCSERVER:N12345:de6acb8e
	res := v.GenerateResponse("de6acb8e")

	// Expected response:
	// $ZRN12345:SERVER:f8ee97157f66455ed6108fccef6ccf5f
	assert.Equal(t, "f8ee97157f66455ed6108fccef6ccf5f", res)
	v.UpdateState(res)

	// Second challenge from server
	// $ZCSERVER:N12345:65b479573b0e
	res = v.GenerateResponse("65b479573b0e")

	// Expected response
	// $ZRN12345:SERVER:8953f545c4e0ffd20943ad89b8ddd087
	assert.Equal(t, "8953f545c4e0ffd20943ad89b8ddd087", res)
	v.UpdateState(res)
}
