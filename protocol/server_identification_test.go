package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestServerIdentificationPDU_Serialize2(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *ServerIdentificationPDU
		wantErr error
	}{
		{
			"Valid",
			"$DISERVER:CLIENT:fsd server:0123456789abcdef\r\n",
			&ServerIdentificationPDU{
				From:             "SERVER",
				To:               "CLIENT",
				Version:          "fsd server",
				InitialChallenge: "0123456789abcdef",
			},
			nil,
		},
		{
			"Missing field",
			"$DI:CLIENT:fsd server:0123456789abcdef\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Non-hexadecimal challenge",
			"$DISERVER:CLIENT:fsd server:ghijklmnop\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Challenge too long",
			"$DISERVER:CLIENT:fsd server:fd9bb85563fc21920f352a74a0917ea88\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParseServerIdentificationPDU(tc.packet)

			// Check the error
			if tc.wantErr != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			// Verify the result
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestServerIdentificationPDU_Serialize(t *testing.T) {
	{
		pdu := ServerIdentificationPDU{
			From:             "SERVER",
			To:               "CLIENT",
			Version:          "fsd server",
			InitialChallenge: "12345",
		}

		s := pdu.Serialize()
		assert.Equal(t, "$DISERVER:CLIENT:fsd server:12345\r\n", s)
	}
}
