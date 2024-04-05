package protocol

import (
	"fmt"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseTextMessagePDU(t *testing.T) {
	V = validator.New()

	tests := []struct {
		name    string
		packet  string
		want    *TextMessagePDU
		wantErr error
	}{
		{
			name:   "Valid message",
			packet: "#TMJOHN:DOE:Hello, world!\r\n",
			want: &TextMessagePDU{
				From:    "JOHN",
				To:      "DOE",
				Message: "Hello, world!",
			},
			wantErr: nil,
		},
		{
			name:    "Invalid from field",
			packet:  "#TMJOHN99999:DOE:Hello, world!\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid to field",
			packet:  "#TMJOHN:DOE1234567:Hello, world!\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Missing to field",
			packet:  "#TMJOHN::Hello, world!\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Missing message",
			packet:  "#TMJOHN:DOE:\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParseTextMessagePDU(tc.packet)

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

func TestTextMessagePDU_Serialize(t *testing.T) {
	tests := []struct {
		name       string
		textPDU    TextMessagePDU
		wantOutput string
	}{
		{
			name: "Valid message",
			textPDU: TextMessagePDU{
				From:    "ALPHA1",
				To:      "BRAVO2",
				Message: "Hello, this is a test message.",
			},
			wantOutput: "#TMALPHA1:BRAVO2:Hello, this is a test message.\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform serialization
			output := tc.textPDU.Serialize()

			// Verify the output
			assert.Equal(t, tc.wantOutput, output, fmt.Sprintf("Test %s: expected serialized output does not match actual output", tc.name))
		})
	}
}
