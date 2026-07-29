package vpilot

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/cilus"
)

// freeSlotRemapReady enables writing long URLs into us_free_slots + rewriting
// CIL ldstr tokens to the free-slot heap offset (R1).
const freeSlotRemapReady = true

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

	// Free slots consumed by remaps in this plan (JWT first, then AFV).
	usedSlots := make(map[string]bool)

	// --- JWT #US ---
	jwtURL, jwtWarns, _ := resolveJWTURL(ep, profile)
	plan.Warnings = append(plan.Warnings, jwtWarns...)
	if jwtURL != "" {
		js, ok := profile.Strings["fsd_jwt"]
		if !ok {
			plan.Blockers = append(plan.Blockers, "profile missing strings.fsd_jwt")
		} else if cilus.FitsBudget(jwtURL, js.PayloadBudgetBytes) {
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
		} else if freeSlotRemapReady {
			need := cilus.BodyBudgetBytes(jwtURL)
			slot, slotID := pickFreeSlot(profile.USFreeSlots, need, usedSlots)
			if slot == nil {
				plan.Blockers = append(plan.Blockers,
					fmt.Sprintf("JWT URL %q (%d runes) exceeds #US budget %d bytes and no free slot fits (%d body bytes needed)",
						jwtURL, len([]rune(jwtURL)), js.PayloadBudgetBytes, need))
				updateConstraintStrategy(plan, "JWTURL", "blocker")
			} else if len(js.LdstrFileOffsets) == 0 {
				plan.Blockers = append(plan.Blockers, "profile strings.fsd_jwt missing ldstr_file_offsets for free-slot remap")
				updateConstraintStrategy(plan, "JWTURL", "blocker")
			} else {
				usedSlots[slotID] = true
				appendFreeSlotRemap(plan, profile, "fsd_jwt", "patch_fsd_jwt", jwtURL, js, *slot, slotID)
				updateConstraintStrategy(plan, "JWTURL", "remap")
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("JWT URL remapped to free #US slot %s (budget %d); stock slot left intact",
						slotID, slot.BudgetBytes))
			}
		} else {
			plan.Blockers = append(plan.Blockers,
				fmt.Sprintf("JWT URL %q (%d runes) exceeds #US budget %d bytes (~%d runes); free-slot remap unavailable; use a short host (≤12 chars for default /api/v1/fsd-jwt) or --prefer-short-jwt if server has fixed /j",
					jwtURL, len([]rune(jwtURL)), js.PayloadBudgetBytes, (js.PayloadBudgetBytes-1)/2))
			updateConstraintStrategy(plan, "JWTURL", "blocker")
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
		} else if cilus.FitsBudget(ep.AFVBaseURL, as.PayloadBudgetBytes) {
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
		} else if freeSlotRemapReady {
			need := cilus.BodyBudgetBytes(ep.AFVBaseURL)
			slot, slotID := pickFreeSlot(profile.USFreeSlots, need, usedSlots)
			if slot == nil || len(as.LdstrFileOffsets) == 0 {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("AFV URL %q exceeds #US budget %d (~%d runes); free-slot remap unavailable — planning -novoice",
						ep.AFVBaseURL, as.PayloadBudgetBytes, (as.PayloadBudgetBytes-1)/2))
				needNoVoice = true
				updateConstraintStrategy(plan, "AFVBaseURL", "launch_novoice")
			} else {
				usedSlots[slotID] = true
				appendFreeSlotRemap(plan, profile, "afv_base", "patch_afv_base", ep.AFVBaseURL, as, *slot, slotID)
				updateConstraintStrategy(plan, "AFVBaseURL", "remap")
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("AFV URL remapped to free #US slot %s (budget %d)", slotID, slot.BudgetBytes))
			}
		} else {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("AFV URL %q exceeds #US budget %d (~%d runes); free-slot remap unavailable — planning -novoice",
					ep.AFVBaseURL, as.PayloadBudgetBytes, (as.PayloadBudgetBytes-1)/2))
			needNoVoice = true
			updateConstraintStrategy(plan, "AFVBaseURL", "launch_novoice")
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
		// Only remind about short paths when we did not already free-slot remap.
		remapped := false
		for _, c := range plan.Constraints {
			if c.Field == "JWTURL" && c.Strategy == "remap" {
				remapped = true
				break
			}
		}
		if !remapped {
			plan.Warnings = append(plan.Warnings,
				"default JWT path /api/v1/fsd-jwt allows max host 12 chars for in-place #US; longer hosts free-slot remap; use --prefer-short-jwt for /j (max host 25 in-place) if server has fixed short JWT routes")
		}
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

// pickFreeSlot returns the first free slot with BudgetBytes >= needBytes that is
// not in used. Slot ID is profile id or a stable index-based fallback.
func pickFreeSlot(slots []clientinject.USFreeSlot, needBytes int, used map[string]bool) (*clientinject.USFreeSlot, string) {
	for i := range slots {
		s := &slots[i]
		id := freeSlotID(*s, i)
		if used[id] {
			continue
		}
		if s.BudgetBytes < needBytes {
			continue
		}
		if s.BodyOffset.Int64() <= 0 || s.HeapOffset.Int64() < 0 {
			continue
		}
		return s, id
	}
	return nil, ""
}

func freeSlotID(s clientinject.USFreeSlot, index int) string {
	if s.ID != "" {
		return s.ID
	}
	return fmt.Sprintf("slot_%d", index)
}

// appendFreeSlotRemap plans a free-slot #US body write + ldstr token rewrite(s).
func appendFreeSlotRemap(
	plan *clientinject.Plan,
	profile *clientinject.Profile,
	stringRef, idPrefix, newURL string,
	spec clientinject.StringSpec,
	slot clientinject.USFreeSlot,
	slotID string,
) {
	plan.Mutations = append(plan.Mutations, clientinject.Mutation{
		ID:          idPrefix + "_freeslot",
		Kind:        clientinject.MutUSHeapString,
		Description: fmt.Sprintf("Write %s URL into free #US slot %s", stringRef, slotID),
		TargetRel:   profile.PrimaryBinary.RelativePath,
		Detail: clientinject.USStringDetail{
			StringRef:   stringRef,
			NewString:   newURL,
			HeapOff:     slot.HeapOffset.Int64(),
			BodyOffs:    []int64{slot.BodyOffset.Int64()},
			BudgetBytes: slot.BudgetBytes,
		},
	})
	tok := makeLdstrToken(slot.HeapOffset.Int64())
	for i, lo := range spec.LdstrFileOffsets {
		off := lo.Int64()
		if off <= 0 {
			continue
		}
		plan.Mutations = append(plan.Mutations, clientinject.Mutation{
			ID:          fmt.Sprintf("%s_ldstr_%d", idPrefix, i),
			Kind:        clientinject.MutLdstrRemap,
			Description: fmt.Sprintf("Remap %s ldstr → free slot heap 0x%X", stringRef, slot.HeapOffset.Int64()),
			TargetRel:   profile.PrimaryBinary.RelativePath,
			Detail: clientinject.LdstrRemapDetail{
				LdstrFileOff: off,
				NewToken:     append([]byte(nil), tok...),
				SlotHeapOff:  slot.HeapOffset.Int64(),
				SlotBodyOff:  slot.BodyOffset.Int64(),
				SlotBudget:   slot.BudgetBytes,
			},
		})
	}
}

// makeLdstrToken builds CIL ldstr opcode + metadata token for a #US heap offset.
// Encoding: 0x72 + little-endian uint32 (0x70000000 | heapOff).
func makeLdstrToken(heapOff int64) []byte {
	tok := uint32(0x70000000 | (uint32(heapOff) & 0x00FFFFFF))
	return []byte{
		0x72,
		byte(tok),
		byte(tok >> 8),
		byte(tok >> 16),
		byte(tok >> 24),
	}
}
