package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseClientQueryResponsePDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *ClientQueryResponsePDU
		wantErr error
	}{
		{
			"Valid with Payload",
			"$CRFROM:TO:FP:P12345\r\n",
			&ClientQueryResponsePDU{
				From:      "FROM",
				To:        "TO",
				QueryType: "FP",
				Payload:   "P12345",
			},
			nil,
		},
		{
			"Valid without Payload",
			"$CRFROM:TO:WH\r\n",
			&ClientQueryResponsePDU{
				From:      "FROM",
				To:        "TO",
				QueryType: "WH",
				Payload:   "",
			},
			nil,
		},
		{
			"Invalid QueryType",
			"$CRFROM:TO:XYZ\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"From field too long",
			"$CRFROMFROM:TO:WH\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing QueryType",
			"$CRFROM:TO:\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Empty packet",
			"\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing From field",
			"$CR:TO:WH\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParseClientQueryResponsePDU(tc.packet)

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

func TestClientQueryResponsePDU_Serialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  *ClientQueryResponsePDU
		want string
	}{
		{
			name: "Serialize with payload",
			pdu: &ClientQueryResponsePDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "IP",
				Payload:   "192.0.2.1",
			},
			want: "$CRCLIENT:SERVER:IP:192.0.2.1\r\n",
		},
		{
			name: "Serialize without payload",
			pdu: &ClientQueryResponsePDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "IP",
				Payload:   "",
			},
			want: "$CRCLIENT:SERVER:IP\r\n",
		},
		{
			name: "Serialize with minimum query type length",
			pdu: &ClientQueryResponsePDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "C?",
				Payload:   "Question",
			},
			want: "$CRCLIENT:SERVER:C?:Question\r\n",
		},
		{
			name: "Serialize with maximum query type length",
			pdu: &ClientQueryResponsePDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "NEWINFO",
				Payload:   "Updates",
			},
			want: "$CRCLIENT:SERVER:NEWINFO:Updates\r\n",
		},
		{
			name: "Serialize with non-alphanumeric characters in payload",
			pdu: &ClientQueryResponsePDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "FP",
				Payload:   "FPL-ABCDE-IS -KJFK0740-N0460F330 BETTE4 BETTE ACK J174 LICKS N76B MICYY SH9",
			},
			want: "$CRCLIENT:SERVER:FP:FPL-ABCDE-IS -KJFK0740-N0460F330 BETTE4 BETTE ACK J174 LICKS N76B MICYY SH9\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform serialization
			result := tc.pdu.Serialize()

			// Verify the result
			assert.Equal(t, tc.want, result)
		})
	}
}
