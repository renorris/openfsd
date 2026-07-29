package xpilot

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/renorris/openfsd/internal/clientinject"
)

// HealthCheck validates PE identity (Verify) then padded-string slots and raw
// length/break sites after Apply.
func (a *Adapter) HealthCheck(install clientinject.Install, ep clientinject.Endpoints) error {
	ep = ep.Normalize()
	w := a.writer()
	pe := clientinject.AbsPrimaryPE(install)
	if pe == "" {
		return fmt.Errorf("xpilot: healthcheck: empty PrimaryPE")
	}

	profile, err := a.loadProfileForInstall(install)
	if err != nil {
		return fmt.Errorf("xpilot: healthcheck profile: %w", err)
	}
	// Refuse unknown / wrong-version PE before reading slots at profile offsets.
	if err := a.Verify(install, profile); err != nil {
		return fmt.Errorf("xpilot: healthcheck verify: %w", err)
	}

	data, err := w.ReadFile(pe)
	if err != nil {
		return fmt.Errorf("xpilot: healthcheck read PE: %w", err)
	}

	statusURL := ep.StatusJSONURL()
	jwtURL := ep.JWTURL()
	endpoints := map[string]string{
		"status_json": statusURL,
		"fsd_jwt":     jwtURL,
		"status":      statusURL,
	}

	for _, m := range profile.Mutations {
		if m.FileOffset == nil {
			continue
		}
		off := m.FileOffset.Int64()
		kind, err := clientinject.MapYAMLKind(m.Kind)
		if err != nil {
			continue
		}
		switch kind {
		case clientinject.MutPaddedString:
			key := strings.TrimSpace(m.EndpointKey)
			if key == "" {
				key = strings.TrimSpace(m.StringRef)
			}
			want := endpoints[key]
			if want == "" {
				continue
			}
			slotLen := 0
			if m.AvailableBytes != nil {
				slotLen = int(m.AvailableBytes.Int64())
			}
			if slotLen <= 0 {
				if s, ok := profile.Strings[key]; ok {
					slotLen = s.PayloadBudgetBytes
				}
			}
			if slotLen <= 0 || off < 0 || int(off)+slotLen > len(data) {
				return fmt.Errorf("xpilot: healthcheck: padded slot %s @%#x out of range", m.ID, off)
			}
			got, err := decodePadded(data[off:int(off)+slotLen], m.Encoding)
			if err != nil {
				return fmt.Errorf("xpilot: healthcheck %s: %w", m.ID, err)
			}
			if got != want {
				return fmt.Errorf("xpilot: healthcheck %s @%#x = %q, want %q", m.ID, off, got, want)
			}
		case clientinject.MutRawOverwrite:
			if lo := strings.TrimSpace(m.LengthOf); lo != "" {
				url := endpoints[lo]
				if url == "" {
					continue
				}
				want := byte(len([]rune(url)))
				if off < 0 || int(off) >= len(data) {
					return fmt.Errorf("xpilot: healthcheck: length site %s out of range", m.ID)
				}
				if data[off] != want {
					return fmt.Errorf("xpilot: healthcheck %s @%#x = %d, want %d", m.ID, off, data[off], want)
				}
				continue
			}
			if len(m.NewBytes) == 0 {
				continue
			}
			if off < 0 || int(off)+len(m.NewBytes) > len(data) {
				return fmt.Errorf("xpilot: healthcheck: raw site %s out of range", m.ID)
			}
			got := data[off : int(off)+len(m.NewBytes)]
			if !bytes.Equal(got, m.NewBytes) {
				return fmt.Errorf("xpilot: healthcheck %s @%#x = %x, want %x", m.ID, off, got, m.NewBytes)
			}
		}
	}
	return nil
}

func decodePadded(slot []byte, encoding string) (string, error) {
	enc := strings.ToLower(strings.TrimSpace(encoding))
	switch enc {
	case "utf16le", "utf-16le", "utf16":
		if len(slot) < 2 {
			return "", fmt.Errorf("utf16 slot too short")
		}
		// Read until U+0000 or end.
		nUnits := len(slot) / 2
		units := make([]uint16, 0, nUnits)
		for i := 0; i < nUnits; i++ {
			u := binary.LittleEndian.Uint16(slot[i*2:])
			if u == 0 {
				break
			}
			units = append(units, u)
		}
		return string(utf16.Decode(units)), nil
	default:
		// utf8/ascii: until first 0x00.
		i := bytes.IndexByte(slot, 0)
		if i < 0 {
			return string(slot), nil
		}
		return string(slot[:i]), nil
	}
}
