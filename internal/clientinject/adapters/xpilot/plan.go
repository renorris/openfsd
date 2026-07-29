package xpilot

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/renorris/openfsd/internal/clientinject"
)

// Plan builds a dry-run mutation plan from the profile. Does not write disk.
func (a *Adapter) Plan(install clientinject.Install, profile *clientinject.Profile, ep clientinject.Endpoints) (*clientinject.Plan, error) {
	if profile == nil {
		return nil, fmt.Errorf("xpilot: nil profile")
	}
	ep = ep.Normalize()
	plan := &clientinject.Plan{
		Install:     install,
		Endpoints:   ep,
		Constraints: a.EndpointConstraints(profile),
	}

	if ep.WebBaseURL == "" {
		plan.Blockers = append(plan.Blockers, "WebBaseURL is required")
	}
	// FSDHost is not written into the PE (status.json feed supplies servers),
	// but keep the form contract: require it so operators configure a coherent pair.
	if ep.FSDHost == "" {
		plan.Blockers = append(plan.Blockers, "FSDHost is required (used with openfsd status feed / operator checklist)")
	}

	// Length immediates are single-byte character counts proven only for ASCII
	// (prior-art examples). Non-ASCII hosts would make UTF-8 byte length diverge
	// from rune count used for the imm — refuse rather than guess.
	if ep.WebBaseURL != "" && !isASCII(ep.WebBaseURL) {
		plan.Blockers = append(plan.Blockers,
			"WebBaseURL must be ASCII; xPilot 3.0.1 PE length immediates are single-byte character counts (non-ASCII hosts unproven)")
	}

	statusURL := ep.StatusJSONURL()
	jwtURL := ep.JWTURL()
	// Prefer full /api/v1/fsd-jwt; PreferShortJWTPath still works via Endpoints.JWTURL.
	if ep.PreferShortJWTPath && jwtURL != "" {
		plan.Warnings = append(plan.Warnings,
			"PreferShortJWTPath set — ensure server has fixed short JWT routes; xPilot slots are large so default /api/v1/fsd-jwt usually fits")
	}

	endpoints := map[string]string{
		"status_json": statusURL,
		"fsd_jwt":     jwtURL,
		"status":      statusURL,
	}

	if statusURL == "" && ep.WebBaseURL != "" {
		plan.Blockers = append(plan.Blockers, "StatusJSONURL empty after normalize")
	}
	if jwtURL == "" && ep.WebBaseURL != "" {
		plan.Blockers = append(plan.Blockers, "JWTURL empty after normalize")
	}

	// Validate endpoint lengths against slots + single-byte length imm.
	if statusURL != "" {
		if err := checkURLFits(statusURL, profile, "status_json", "utf8"); err != nil {
			plan.Blockers = append(plan.Blockers, err.Error())
			updateConstraintStrategy(plan, "StatusJSONURL", "blocker")
		}
	}
	if jwtURL != "" {
		if err := checkURLFits(jwtURL, profile, "fsd_jwt", "utf16le"); err != nil {
			plan.Blockers = append(plan.Blockers, err.Error())
			updateConstraintStrategy(plan, "JWTURL", "blocker")
		}
	}

	if len(profile.Mutations) == 0 {
		plan.Blockers = append(plan.Blockers, "profile has no mutations (research incomplete)")
		return plan, nil
	}

	for _, m := range profile.Mutations {
		if m.FileOffset == nil {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: missing file_offset", m.ID))
			continue
		}
		off := m.FileOffset.Int64()
		if off <= 0 {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: invalid file_offset %d", m.ID, off))
			continue
		}
		kind, err := clientinject.MapYAMLKind(m.Kind)
		if err != nil {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: %v", m.ID, err))
			continue
		}

		switch kind {
		case clientinject.MutPaddedString:
			key := strings.TrimSpace(m.EndpointKey)
			if key == "" {
				key = strings.TrimSpace(m.StringRef)
			}
			url := endpoints[key]
			if url == "" {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: unknown/empty endpoint_key %q", m.ID, key))
				continue
			}
			slotLen := 0
			if m.AvailableBytes != nil {
				slotLen = int(m.AvailableBytes.Int64())
			}
			if slotLen <= 0 {
				if s, ok := profile.Strings[key]; ok && s.PayloadBudgetBytes > 0 {
					slotLen = s.PayloadBudgetBytes
				}
			}
			if slotLen <= 0 {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: missing available_bytes", m.ID))
				continue
			}
			enc := m.Encoding
			if enc == "" {
				enc = "utf8"
			}
			plan.Mutations = append(plan.Mutations, clientinject.Mutation{
				ID:          m.ID,
				Kind:        clientinject.MutPaddedString,
				Description: m.Description,
				TargetRel:   profile.PrimaryBinary.RelativePath,
				Detail: clientinject.PaddedStringDetail{
					FileOffset: off,
					NewString:  url,
					SlotLen:    slotLen,
					Encoding:   enc,
				},
			})

		case clientinject.MutRawOverwrite:
			if lo := strings.TrimSpace(m.LengthOf); lo != "" {
				url := endpoints[lo]
				if url == "" {
					plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: length_of %q empty", m.ID, lo))
					continue
				}
				n := utf8.RuneCountInString(url)
				if n > maxLengthImm {
					plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: URL length %d exceeds single-byte imm max %d", m.ID, n, maxLengthImm))
					continue
				}
				plan.Mutations = append(plan.Mutations, clientinject.Mutation{
					ID:          m.ID,
					Kind:        clientinject.MutRawOverwrite,
					Description: m.Description,
					TargetRel:   profile.PrimaryBinary.RelativePath,
					Detail: clientinject.RawOverwriteDetail{
						FileOffset: off,
						NewBytes:   []byte{byte(n)},
					},
				})
				continue
			}
			if len(m.NewBytes) == 0 {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: raw_overwrite needs new_bytes or length_of", m.ID))
				continue
			}
			plan.Mutations = append(plan.Mutations, clientinject.Mutation{
				ID:          m.ID,
				Kind:        clientinject.MutRawOverwrite,
				Description: m.Description,
				TargetRel:   profile.PrimaryBinary.RelativePath,
				Detail: clientinject.RawOverwriteDetail{
					FileOffset: off,
					NewBytes:   append([]byte(nil), m.NewBytes...),
				},
			})

		default:
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("mutation %s: unsupported kind %q for xpilot adapter", m.ID, m.Kind))
		}
	}

	if ep.AFVBaseURL != "" {
		plan.Warnings = append(plan.Warnings,
			"AFVBaseURL set but xPilot 3.0.1 adapter does not retarget voice; configure AFV outside this PE patch or use a future profile")
	}
	if ep.ForceDisableAFV {
		plan.Warnings = append(plan.Warnings,
			"ForceDisableAFV has no PE/launch effect for xPilot 3.0.1 (no -novoice equivalent in prior art)")
	}

	return plan, nil
}

func checkURLFits(url string, profile *clientinject.Profile, stringKey, encoding string) error {
	if !isASCII(url) {
		return fmt.Errorf("%s URL must be ASCII; xPilot 3.0.1 length immediates are single-byte character counts (non-ASCII unproven)", stringKey)
	}
	n := utf8.RuneCountInString(url) // equals len(url) for ASCII
	if n > maxLengthImm {
		return fmt.Errorf("%s URL length %d exceeds single-byte length immediate max %d", stringKey, n, maxLengthImm)
	}
	budget := 0
	if s, ok := profile.Strings[stringKey]; ok {
		budget = s.PayloadBudgetBytes
	}
	if budget <= 0 {
		return nil
	}
	enc := strings.ToLower(encoding)
	var need int
	switch enc {
	case "utf16le", "utf-16le", "utf16":
		need = (n + 1) * 2 // runes + NUL
	default:
		// utf8/ascii: bytes + NUL (ASCII: runes == bytes).
		need = len(url) + 1
	}
	if need > budget {
		return fmt.Errorf("%s URL needs %d encoded bytes > slot budget %d", stringKey, need, budget)
	}
	return nil
}

// isASCII reports whether s contains only bytes < 128 (openfsd production hosts).
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func updateConstraintStrategy(plan *clientinject.Plan, field, strategy string) {
	for i := range plan.Constraints {
		if plan.Constraints[i].Field == field {
			plan.Constraints[i].Strategy = strategy
			return
		}
	}
	plan.Constraints = append(plan.Constraints, clientinject.Constraint{
		Field:    field,
		Strategy: strategy,
	})
}
