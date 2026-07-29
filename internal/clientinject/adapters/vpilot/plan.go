package vpilot

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/cilus"
)

// freeSlotRemapReady is false until R1 wires ldstr token bytes + free-slot body
// writes. When false, non-empty us_free_slots are treated as unavailable
// (JWT blocker / AFV -novoice) rather than planning half-implemented remaps.
const freeSlotRemapReady = false

// Plan builds a dry-run mutation plan. Does not write disk.
func (a *Adapter) Plan(install clientinject.Install, profile *clientinject.Profile, ep clientinject.Endpoints) (*clientinject.Plan, error) {
	if profile == nil {
		return nil, fmt.Errorf("vpilot: nil profile")
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
	if ep.FSDHost == "" {
		plan.Blockers = append(plan.Blockers, "FSDHost is required")
	}

	cfgPaths := a.resolveConfigWritePaths(install)
	anyExist := false
	for _, p := range cfgPaths {
		if _, err := a.writer().Stat(p); err == nil {
			anyExist = true
			break
		}
	}
	if !anyExist {
		plan.Warnings = append(plan.Warnings,
			"no vPilotConfig.xml found yet; Apply will create "+filepath.Join(install.RootDir, configName)+
				" (run vPilot once on a real install so the client owns other settings)")
	}

	// --- Config rewrite ---
	statusURL := ep.StatusURL()
	servers := []string{ep.CachedServerEntry()}
	plan.Mutations = append(plan.Mutations, clientinject.Mutation{
		ID:          "config_status_servers",
		Kind:        clientinject.MutConfigRewrite,
		Description: "Rewrite NetworkStatusURL + CachedServers; clear credentials (preserve other XML)",
		TargetRel:   configName,
		Detail: clientinject.ConfigRewriteDetail{
			Paths:            cfgPaths,
			NetworkStatusURL: statusURL,
			CachedServers:    servers,
			ClearCredentials: true,
		},
	})

	// --- JWT #US ---
	jwtURL, jwtWarns, _ := resolveJWTURL(ep, profile)
	plan.Warnings = append(plan.Warnings, jwtWarns...)
	if jwtURL != "" {
		js, ok := profile.Strings["fsd_jwt"]
		if !ok {
			plan.Blockers = append(plan.Blockers, "profile missing strings.fsd_jwt")
		} else if !cilus.FitsBudget(jwtURL, js.PayloadBudgetBytes) {
			// Free-slot remap not ready (R1) — same as empty us_free_slots.
			if len(profile.USFreeSlots) > 0 && !freeSlotRemapReady {
				plan.Warnings = append(plan.Warnings,
					"us_free_slots present but ldstr remap not implemented (R1 incomplete); treating free-slot remap as unavailable")
			}
			if freeSlotRemapReady && len(profile.USFreeSlots) > 0 {
				// Reserved for full R1 implementation (must set NewToken + body string).
				plan.Blockers = append(plan.Blockers, "JWT free-slot remap ready flag set but not fully implemented")
				updateConstraintStrategy(plan, "JWTURL", "blocker")
			} else {
				plan.Blockers = append(plan.Blockers,
					fmt.Sprintf("JWT URL %q (%d runes) exceeds #US budget %d bytes (~%d runes); free-slot remap unavailable; use a short host (≤12 chars for default /api/v1/fsd-jwt) or --prefer-short-jwt if server has fixed /j",
						jwtURL, len([]rune(jwtURL)), js.PayloadBudgetBytes, (js.PayloadBudgetBytes-1)/2))
				updateConstraintStrategy(plan, "JWTURL", "blocker")
			}
		} else {
			bodyOffs := flexibleToInt64s(js.BodyFileOffsets)
			plan.Mutations = append(plan.Mutations, clientinject.Mutation{
				ID:          "patch_fsd_jwt",
				Kind:        clientinject.MutUSHeapString,
				Description: "Retarget FSD JWT #US in-place",
				TargetRel:   profile.PrimaryBinary.RelativePath,
				Detail: clientinject.USStringDetail{
					StringRef:   "fsd_jwt",
					NewString:   jwtURL,
					HeapOff:     js.USHeapOffset.Int64(),
					BodyOffs:    bodyOffs,
					BudgetBytes: js.PayloadBudgetBytes,
				},
			})
			updateConstraintStrategy(plan, "JWTURL", "in_place")
		}
	}

	// --- AFV #US or -novoice ---
	needNoVoice := ep.ForceDisableAFV
	if ep.ForceDisableAFV || ep.AFVBaseURL == "" {
		needNoVoice = true
		if ep.AFVBaseURL == "" && !ep.ForceDisableAFV {
			plan.Warnings = append(plan.Warnings, "AFVBaseURL empty — planning -novoice (no voice)")
		}
		if ep.ForceDisableAFV {
			updateConstraintStrategy(plan, "AFVBaseURL", "launch_novoice")
		}
	} else {
		as, ok := profile.Strings["afv_base"]
		if !ok {
			plan.Warnings = append(plan.Warnings, "profile missing strings.afv_base; planning -novoice")
			needNoVoice = true
			updateConstraintStrategy(plan, "AFVBaseURL", "launch_novoice")
		} else if !cilus.FitsBudget(ep.AFVBaseURL, as.PayloadBudgetBytes) {
			if freeSlotRemapReady && len(profile.USFreeSlots) > 0 {
				plan.Blockers = append(plan.Blockers, "AFV free-slot remap ready flag set but not fully implemented")
				updateConstraintStrategy(plan, "AFVBaseURL", "blocker")
			} else {
				if len(profile.USFreeSlots) > 0 && !freeSlotRemapReady {
					plan.Warnings = append(plan.Warnings,
						"us_free_slots present but ldstr remap not implemented (R1 incomplete); AFV falls back to -novoice")
				}
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("AFV URL %q exceeds #US budget %d (~%d runes); free-slot remap unavailable — planning -novoice",
						ep.AFVBaseURL, as.PayloadBudgetBytes, (as.PayloadBudgetBytes-1)/2))
				needNoVoice = true
				updateConstraintStrategy(plan, "AFVBaseURL", "launch_novoice")
			}
		} else {
			bodyOffs := flexibleToInt64s(as.BodyFileOffsets)
			plan.Mutations = append(plan.Mutations, clientinject.Mutation{
				ID:          "patch_afv_base",
				Kind:        clientinject.MutUSHeapString,
				Description: "Retarget AFV base #US in-place",
				TargetRel:   profile.PrimaryBinary.RelativePath,
				Detail: clientinject.USStringDetail{
					StringRef:   "afv_base",
					NewString:   ep.AFVBaseURL,
					HeapOff:     as.USHeapOffset.Int64(),
					BodyOffs:    bodyOffs,
					BudgetBytes: as.PayloadBudgetBytes,
				},
			})
			updateConstraintStrategy(plan, "AFVBaseURL", "in_place")
		}
	}

	// afv_disable_pe: only if profile has non-null file_offset (R2).
	for _, m := range profile.Mutations {
		if m.Kind != "raw_overwrite" && m.Kind != "afv_disable_pe" {
			continue
		}
		if m.OnlyIf != "" && m.OnlyIf != "pe_disable_selected_and_offset_set" {
			continue
		}
		if m.FileOffset == nil || m.FileOffset.Int64() == 0 {
			continue
		}
		if needNoVoice && ep.ForceDisableAFV {
			plan.Mutations = append(plan.Mutations, clientinject.Mutation{
				ID:          m.ID,
				Kind:        clientinject.MutAFVDisablePE,
				Description: m.Description,
				TargetRel:   profile.PrimaryBinary.RelativePath,
				Detail: clientinject.RawOverwriteDetail{
					FileOffset: m.FileOffset.Int64(),
					NewBytes:   append([]byte(nil), m.NewBytes...),
				},
			})
		}
	}

	// Launch flags (recorded in plan only — not PE).
	var launchArgs []string
	if addr := ep.FSDAddress(); addr != "" {
		flag := profile.Launch.ServerAddressFlag
		if flag == "" {
			flag = "-serveraddressoverride"
		}
		launchArgs = append(launchArgs, flag, addr)
	}
	if needNoVoice {
		nv := "-novoice"
		if len(profile.Launch.NoVoiceFlags) > 0 {
			nv = profile.Launch.NoVoiceFlags[0]
		}
		launchArgs = append(launchArgs, nv)
	}
	if len(launchArgs) > 0 {
		plan.Mutations = append(plan.Mutations, clientinject.Mutation{
			ID:          "launch_flags",
			Kind:        clientinject.MutLaunchFlag,
			Description: "Launch flags for openfsd (not written to PE)",
			Detail:      clientinject.LaunchFlagDetail{Args: launchArgs},
		})
	}

	if !ep.PreferShortJWTPath && ep.WebBaseURL != "" {
		plan.Warnings = append(plan.Warnings,
			"default JWT path /api/v1/fsd-jwt allows max host 12 chars for in-place #US; use --prefer-short-jwt for /j (max host 25) if server has fixed short JWT routes")
	}

	return plan, nil
}

// resolveJWTURL picks a JWT URL that fits the profile budget when possible.
func resolveJWTURL(ep clientinject.Endpoints, profile *clientinject.Profile) (url string, warns []string, blocker string) {
	if ep.WebBaseURL == "" {
		return "", nil, ""
	}
	js, ok := profile.Strings["fsd_jwt"]
	budget := 71
	if ok && js.PayloadBudgetBytes > 0 {
		budget = js.PayloadBudgetBytes
	}

	candidates := jwtURLCandidates(ep)
	if len(candidates) == 0 {
		return "", nil, ""
	}
	for _, c := range candidates {
		if cilus.FitsBudget(c, budget) {
			if ep.PreferShortJWTPath && c != ep.WebBaseURL+"/api/v1/fsd-jwt" {
				warns = append(warns,
					"PreferShortJWTPath set without connectivity check — ensure server has fixed /j (direct handler, not 302) or A8 aliases")
			}
			return c, warns, ""
		}
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if len(c) < len(best) {
			best = c
		}
	}
	if ep.PreferShortJWTPath {
		warns = append(warns,
			"PreferShortJWTPath set but no candidate JWT URL fits #US budget (even /j); free-slot remap required or shorten hostname")
	}
	return best, warns, ""
}

// jwtURLCandidates returns URLs to try (shortest first when PreferShortJWTPath).
func jwtURLCandidates(ep clientinject.Endpoints) []string {
	base := strings.TrimRight(strings.TrimSpace(ep.WebBaseURL), "/")
	if base == "" {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	add := func(u string) {
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	if ep.PreferShortJWTPath {
		cands := ep.ShortJWTPathCandidates
		if len(cands) == 0 {
			cands = clientinject.DefaultShortJWTPathCandidates()
		}
		for _, p := range cands {
			if !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
			add(base + p)
		}
	}
	add(base + "/api/v1/fsd-jwt")
	return out
}

func flexibleToInt64s(ff []clientinject.FlexibleInt64) []int64 {
	out := make([]int64, len(ff))
	for i, f := range ff {
		out[i] = f.Int64()
	}
	return out
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
