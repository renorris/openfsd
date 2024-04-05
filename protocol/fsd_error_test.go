package protocol

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseNetworkErrorPDU(t *testing.T) {

	// Normal error test
	{
		p := "$ERN123:SERVER:0::this is a test error packet\r\n"
		pdu, err := ParseNetworkErrorPDU(p)
		assert.Nil(t, err)
		assert.NotNil(t, pdu)
		assert.Equal(t, "N123", pdu.From)
		assert.Equal(t, "SERVER", pdu.To)
		assert.Equal(t, ErrorCode(0), pdu.Code)
		assert.Equal(t, "", pdu.Param)
		assert.Equal(t, "this is a test error packet", pdu.Message)
	}

	// Test with several colons in message
	{
		p := "$ERN123:SERVER:0::here:are:a:lot:of:colons\r\n"
		pdu, err := ParseNetworkErrorPDU(p)
		assert.Nil(t, err)
		assert.NotNil(t, pdu)
		assert.Equal(t, "N123", pdu.From)
		assert.Equal(t, "SERVER", pdu.To)
		assert.Equal(t, ErrorCode(0), pdu.Code)
		assert.Equal(t, "", pdu.Param)
		assert.Equal(t, "here:are:a:lot:of:colons", pdu.Message)
	}

	// Missing field
	{
		p := "$ERN123:SERVER::here:are:a:lot:of:colons\r\n"
		pdu, err := ParseNetworkErrorPDU(p)
		assert.NotNil(t, err)
		assert.IsType(t, &FSDError{}, err)
		assert.Nil(t, pdu)
	}

	// Non-integer error code
	{
		p := "$ERN123:SERVER:this-is-clearly-not-a-number::this is a test error packet\r\n"
		pdu, err := ParseNetworkErrorPDU(p)
		assert.NotNil(t, err)
		assert.IsType(t, &FSDError{}, err)
		assert.Nil(t, pdu)
	}
}

func TestFSDError_Serialize(t *testing.T) {
	{
		pdu := FSDError{
			From:    "N123",
			To:      "SERVER",
			Code:    5,
			Param:   "",
			Message: "this is a test error packet",
		}
		s := pdu.Serialize()
		assert.Equal(t, "$ERN123:SERVER:005::this is a test error packet\r\n", s)
	}
}
