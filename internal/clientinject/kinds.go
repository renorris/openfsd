package clientinject

import (
	"fmt"
	"strings"
)

// MutationKind identifies how a plan mutation is applied.
type MutationKind string

const (
	MutConfigRewrite MutationKind = "config_rewrite"
	MutUSHeapString  MutationKind = "cil_us_string"
	MutLdstrRemap    MutationKind = "cil_ldstr_remap"
	MutRawOverwrite  MutationKind = "raw_overwrite"
	MutPaddedString  MutationKind = "padded_string"
	MutAFVDisablePE  MutationKind = "afv_disable_pe"
	MutLaunchFlag    MutationKind = "launch_flag"
)

// MapYAMLKind maps a profile YAML kind string to a MutationKind.
// Special kinds that expand at plan time (cil_us_or_remap, afv_disable) return
// an error directing the adapter/planner to expand them — except when a
// direct mapping is unambiguous.
func MapYAMLKind(yamlKind string) (MutationKind, error) {
	k := strings.ToLower(strings.TrimSpace(yamlKind))
	switch k {
	case "vpilot_config", "config_rewrite":
		return MutConfigRewrite, nil
	case "cil_us_string":
		return MutUSHeapString, nil
	case "cil_ldstr_remap":
		return MutLdstrRemap, nil
	case "raw_overwrite", "section_overwrite":
		return MutRawOverwrite, nil
	case "padded_string":
		return MutPaddedString, nil
	case "afv_disable_pe":
		return MutAFVDisablePE, nil
	case "launch_flag":
		return MutLaunchFlag, nil
	case "cil_us_or_remap":
		// Planner expands to cil_us_string and/or cil_ldstr_remap.
		return "", fmt.Errorf("clientinject: yaml kind %q must be expanded by planner (in-place and/or remap)", yamlKind)
	case "afv_disable":
		// PE only if offset set, else launch_flag -novoice — adapter expands.
		return "", fmt.Errorf("clientinject: yaml kind %q must be expanded by adapter (afv_disable_pe or launch_flag)", yamlKind)
	case "":
		return "", fmt.Errorf("clientinject: empty yaml kind")
	default:
		return "", fmt.Errorf("clientinject: unknown yaml kind %q", yamlKind)
	}
}

// ExpandYAMLKind returns one or more MutationKinds for planner use.
// For direct kinds it returns a single-element slice; for expandable kinds it
// returns the candidate kinds the planner should choose among.
func ExpandYAMLKind(yamlKind string) ([]MutationKind, error) {
	k := strings.ToLower(strings.TrimSpace(yamlKind))
	switch k {
	case "cil_us_or_remap":
		return []MutationKind{MutUSHeapString, MutLdstrRemap}, nil
	case "afv_disable":
		return []MutationKind{MutAFVDisablePE, MutLaunchFlag}, nil
	default:
		mk, err := MapYAMLKind(yamlKind)
		if err != nil {
			return nil, err
		}
		return []MutationKind{mk}, nil
	}
}
