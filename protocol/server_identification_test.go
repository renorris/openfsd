package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
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
			"$DISERVER:CLIENT:server server:0123456789abcdef\r\n",
			&ServerIdentificationPDU{
				From:             "SERVER",
				To:               "CLIENT",
				Version:          "server server",
				InitialChallenge: "0123456789abcdef",
			},
			nil,
		},
		{
			"Missing field",
			"$DI:CLIENT:server server:0123456789abcdef\r\n",
			&ServerIdentificationPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Non-hexadecimal challenge",
			"$DISERVER:CLIENT:server server:ghijklmnop\r\n",
			&ServerIdentificationPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Challenge too long",
			"$DISERVER:CLIENT:server server:fd9bb85563fc21920f352a74a0917ea88\r\n",
			&ServerIdentificationPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := ServerIdentificationPDU{}
			err := pdu.Parse(tc.packet)

			// Check the error
			if tc.wantErr != nil {
				if strings.Contains(tc.wantErr.Error(), "validation error") {
					assert.Contains(t, err.Error(), "validation error")
				} else {
					assert.EqualError(t, err, tc.wantErr.Error())
				}
			} else {
				assert.NoError(t, err)
			}

			// Verify the result
			assert.Equal(t, tc.want, &pdu)
		})
	}
}

func TestServerIdentificationPDU_Serialize(t *testing.T) {
	{
		pdu := ServerIdentificationPDU{
			From:             "SERVER",
			To:               "CLIENT",
			Version:          "server server",
			InitialChallenge: "12345",
		}

		s := pdu.Serialize()
		assert.Equal(t, "$DISERVER:CLIENT:server server:12345\r\n", s)
	}
}
