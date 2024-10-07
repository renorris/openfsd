package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParseClientQueryPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *ClientQueryPDU
		wantErr error
	}{
		{
			"Valid with Payload",
			"$CQFROM:TO:FP:P12345\r\n",
			&ClientQueryPDU{
				From:      "FROM",
				To:        "TO",
				QueryType: "FP",
				Payload:   "P12345",
			},
			nil,
		},
		{
			"Valid without Payload",
			"$CQFROM:TO:WH\r\n",
			&ClientQueryPDU{
				From:      "FROM",
				To:        "TO",
				QueryType: "WH",
				Payload:   "",
			},
			nil,
		},
		{
			"Invalid QueryType",
			"$CQFROM:TO:XYZ\r\n",
			&ClientQueryPDU{},
			NewGenericFSDError(SyntaxError, "XYZ", "invalid query type"),
		},
		{
			"From field too long",
			"$CQFROMFROMFROMFROMFROMFROMFROM:TO:WH\r\n",
			&ClientQueryPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing QueryType",
			"$CQFROM:TO:\r\n",
			&ClientQueryPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Empty packet",
			"\r\n",
			&ClientQueryPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			"Missing From field",
			"$CQ:TO:WH\r\n",
			&ClientQueryPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := ClientQueryPDU{}
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

func TestClientQueryPDU_Serialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  *ClientQueryPDU
		want string
	}{
		{
			name: "Serialize with payload",
			pdu: &ClientQueryPDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "IP",
				Payload:   "192.0.2.1",
			},
			want: "$CQCLIENT:SERVER:IP:192.0.2.1\r\n",
		},
		{
			name: "Serialize without payload",
			pdu: &ClientQueryPDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "IP",
				Payload:   "",
			},
			want: "$CQCLIENT:SERVER:IP\r\n",
		},
		{
			name: "Serialize with minimum query type length",
			pdu: &ClientQueryPDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "C?",
				Payload:   "Question",
			},
			want: "$CQCLIENT:SERVER:C?:Question\r\n",
		},
		{
			name: "Serialize with maximum query type length",
			pdu: &ClientQueryPDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "NEWINFO",
				Payload:   "Updates",
			},
			want: "$CQCLIENT:SERVER:NEWINFO:Updates\r\n",
		},
		{
			name: "Serialize with non-alphanumeric characters in payload",
			pdu: &ClientQueryPDU{
				From:      "CLIENT",
				To:        "SERVER",
				QueryType: "FP",
				Payload:   "FPL-ABCDE-IS -KJFK0740-N0460F330 BETTE4 BETTE ACK J174 LICKS N76B MICYY SH9",
			},
			want: "$CQCLIENT:SERVER:FP:FPL-ABCDE-IS -KJFK0740-N0460F330 BETTE4 BETTE ACK J174 LICKS N76B MICYY SH9\r\n",
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
