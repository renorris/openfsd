package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAddPilotPDU_Serialize(t *testing.T) {
	// Define test case with expected serialization output
	tests := []struct {
		name    string
		pdu     AddPilotPDU
		wantStr string
	}{
		{
			name: "Valid Serialization",
			pdu: AddPilotPDU{
				From:             "CLIENT",
				To:               "SERVER",
				CID:              1234567,
				Token:            "jwtTokenExample",
				NetworkRating:    5,
				ProtocolRevision: 2,
				SimulatorType:    3,
				RealName:         "John Smith",
			},
			wantStr: "#APCLIENT:SERVER:1234567:jwtTokenExample:5:2:3:John Smith\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform serialization
			gotStr := tc.pdu.Serialize()

			// Assert that the serialization output matches the expected string
			assert.Equal(t, tc.wantStr, gotStr)
		})
	}
}

func TestParseAddPilotPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *AddPilotPDU
		wantErr error
	}{
		{
			"Valid",
			"#APCLIENT:SERVER:1234567:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8:5:2:3:John Smith\r\n",
			&AddPilotPDU{
				From:             "CLIENT",
				To:               "SERVER",
				CID:              1234567,
				Token:            "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8",
				NetworkRating:    5,
				ProtocolRevision: 2,
				SimulatorType:    3,
				RealName:         "John Smith",
			},
			nil,
		},
		{
			"Invalid CID length",
			"#APCLIENT:SERVER:12345:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8:5:2:3:John Smith\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid CID format",
			"#APCLIENT:SERVER:ABCDE12:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8:5:2:3:John Smith\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid Network Rating",
			"#APCLIENT:SERVER:1234567:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8:13:2:3:John Smith\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid Simulator Type",
			"#APCLIENT:SERVER:1234567:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8:5:2:7:John Smith\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Real name too long",
			"#APCLIENT:SERVER:1234567:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8:5:2:3:This Is Way Too Long For A Full Real Name\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing Delimeters",
			"APCLIENTSERVER1234567eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEwMDAwMDAiLCJhdWQiOlsiZnNkLWxpdmUiXSwiZXhwIjoxNzExOTA4OTM4LCJuYmYiOjE3MTE5MDgzOTgsImlhdCI6MTcxMTkwODUxOCwianRpIjoiRDFCS1BPdUdKelAzZE5NdnV6d1JNZz09IiwiY29udHJvbGxlcl9yYXRpbmciOjAsInBpbG90X3JhdGluZyI6MH0.kg23HhANM6aUI9mRUUGX-Vx8HKjTpzkDxOXlvWkjnC8523John Smith",
			nil,
			NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParseAddPilotPDU(tc.packet)

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
