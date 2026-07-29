package clientinject

import (
	"testing"
)

func TestMapYAMLKind(t *testing.T) {
	tests := []struct {
		in      string
		want    MutationKind
		wantErr bool
	}{
		{"vpilot_config", MutConfigRewrite, false},
		{"config_rewrite", MutConfigRewrite, false},
		{"cil_us_string", MutUSHeapString, false},
		{"cil_ldstr_remap", MutLdstrRemap, false},
		{"raw_overwrite", MutRawOverwrite, false},
		{"section_overwrite", MutRawOverwrite, false},
		{"padded_string", MutPaddedString, false},
		{"afv_disable_pe", MutAFVDisablePE, false},
		{"launch_flag", MutLaunchFlag, false},
		{"cil_us_or_remap", "", true},
		{"afv_disable", "", true},
		{"", "", true},
		{"nope", "", true},
	}
	for _, tc := range tests {
		got, err := MapYAMLKind(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("MapYAMLKind(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("MapYAMLKind(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("MapYAMLKind(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestExpandYAMLKind(t *testing.T) {
	got, err := ExpandYAMLKind("cil_us_or_remap")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != MutUSHeapString || got[1] != MutLdstrRemap {
		t.Fatalf("got %v", got)
	}
	got, err = ExpandYAMLKind("afv_disable")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != MutAFVDisablePE || got[1] != MutLaunchFlag {
		t.Fatalf("got %v", got)
	}
	got, err = ExpandYAMLKind("config_rewrite")
	if err != nil || len(got) != 1 || got[0] != MutConfigRewrite {
		t.Fatalf("got %v err %v", got, err)
	}
}
