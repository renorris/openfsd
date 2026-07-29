package clientinject

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ProcessRunner executes a client binary. Injectable for tests so shadow
// launch can be exercised without a real PE on the host.
type ProcessRunner interface {
	// Run starts name with args and working directory dir, waiting until exit.
	Run(ctx context.Context, name string, args []string, dir string) error
}

// DefaultProcessRunner runs the process via os/exec with stdio inherited.
type DefaultProcessRunner struct{}

// Run implements ProcessRunner.
func (DefaultProcessRunner) Run(ctx context.Context, name string, args []string, dir string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clientinject: run %s: %w", name, err)
	}
	return nil
}

// LaunchArgsFromPlan extracts launch CLI args from MutLaunchFlag mutations.
// Returns nil when the plan has no launch_flag mutation.
func LaunchArgsFromPlan(plan *Plan) []string {
	if plan == nil {
		return nil
	}
	for _, m := range plan.Mutations {
		if m.Kind != MutLaunchFlag {
			continue
		}
		switch d := m.Detail.(type) {
		case LaunchFlagDetail:
			return append([]string(nil), d.Args...)
		case *LaunchFlagDetail:
			if d != nil {
				return append([]string(nil), d.Args...)
			}
		}
	}
	return nil
}

// IsPEMutationKind reports whether kind mutates a PE/DLL binary (not config/launch).
func IsPEMutationKind(k MutationKind) bool {
	switch k {
	case MutUSHeapString, MutLdstrRemap, MutRawOverwrite, MutPaddedString, MutAFVDisablePE:
		return true
	default:
		return false
	}
}
