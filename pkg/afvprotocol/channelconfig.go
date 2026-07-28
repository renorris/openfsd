package afvprotocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// ChannelConfig is the voice session channel configuration as returned in
// PostCallsignResponse (client-perspective key names).
type ChannelConfig struct {
	ChannelTag      string  `json:"channelTag"`
	AeadReceiveKey  []byte  `json:"-"` // 32 bytes; JSON via base64 helpers
	AeadTransmitKey []byte  `json:"-"`
	HmacKey         *string `json:"hmacKey"` // unused; may be null
}

// channelConfigJSON is the wire JSON form with base64 key fields.
type channelConfigJSON struct {
	ChannelTag      string  `json:"channelTag"`
	AeadReceiveKey  string  `json:"aeadReceiveKey"`
	AeadTransmitKey string  `json:"aeadTransmitKey"`
	HmacKey         *string `json:"hmacKey"`
}

var errChannelConfigKey = errors.New("afvprotocol: channel config key must decode to 32 bytes")

// MarshalJSON encodes keys as standard base64 (AFV-Native / nlohmann default).
func (c ChannelConfig) MarshalJSON() ([]byte, error) {
	j := channelConfigJSON{
		ChannelTag:      c.ChannelTag,
		AeadReceiveKey:  base64.StdEncoding.EncodeToString(c.AeadReceiveKey),
		AeadTransmitKey: base64.StdEncoding.EncodeToString(c.AeadTransmitKey),
		HmacKey:         c.HmacKey,
	}
	return json.Marshal(j)
}

// UnmarshalJSON decodes base64 keys and validates length 32.
func (c *ChannelConfig) UnmarshalJSON(data []byte) error {
	var j channelConfigJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	rx, err := base64.StdEncoding.DecodeString(j.AeadReceiveKey)
	if err != nil {
		return fmt.Errorf("aeadReceiveKey: %w", err)
	}
	tx, err := base64.StdEncoding.DecodeString(j.AeadTransmitKey)
	if err != nil {
		return fmt.Errorf("aeadTransmitKey: %w", err)
	}
	if len(rx) != KeySize || len(tx) != KeySize {
		return errChannelConfigKey
	}
	c.ChannelTag = j.ChannelTag
	c.AeadReceiveKey = rx
	c.AeadTransmitKey = tx
	c.HmacKey = j.HmacKey
	return nil
}

// DecodeChannelConfigJSON parses ChannelConfig from JSON bytes.
func DecodeChannelConfigJSON(data []byte) (ChannelConfig, error) {
	var c ChannelConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return ChannelConfig{}, err
	}
	return c, nil
}

// EncodeChannelConfigJSON returns JSON for ChannelConfig.
func EncodeChannelConfigJSON(c ChannelConfig) ([]byte, error) {
	return json.Marshal(c)
}
