package clientinject

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// ProcessRunner executes a client binary. Injectable for tests so shadow
// launch can be exercised without a real PE on the host.
type ProcessRunner interface {
	// Run starts name with args and working directory dir, waiting until exit.
	// Non-zero process exit should return an error wrapping ErrProcessExit
	// (see ProcessExitError) so callers can distinguish launch/patch failures
	// from the client process itself exiting non-zero.
	Run(ctx context.Context, name string, args []string, dir string) error
}

// ErrProcessExit indicates the client process started but exited non-zero.
// It is distinct from prepare/apply failures.
var ErrProcessExit = errors.New("clientinject: client process exited non-zero")

// ProcessExitError wraps a non-zero process exit from ProcessRunner.
type ProcessExitError struct {
	Name     string
	ExitCode int
	Err      error
}

func (e *ProcessExitError) Error() string {
	if e == nil {
		return "clientinject: client process exited non-zero"
	}
	if e.ExitCode != 0 {
		return fmt.Sprintf("clientinject: %s exited with code %d", e.Name, e.ExitCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("clientinject: run %s: %v", e.Name, e.Err)
	}
	return "clientinject: client process exited non-zero"
}

func (e *ProcessExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Err != nil {
		return e.Err
	}
	return ErrProcessExit
}

// Is reports equality with ErrProcessExit.
func (e *ProcessExitError) Is(target error) bool {
	return target == ErrProcessExit
}

// DefaultProcessRunner runs the process via os/exec with stdio inherited.
// Respects ctx cancellation (CommandContext kills the child).
type DefaultProcessRunner struct{}

// Run implements ProcessRunner.
func (DefaultProcessRunner) Run(ctx context.Context, name string, args []string, dir string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &ProcessExitError{
				Name:     name,
				ExitCode: ee.ExitCode(),
				Err:      err,
			}
		}
		// ctx cancel, start failure, etc.
		if ctx.Err() != nil {
			return fmt.Errorf("clientinject: run %s: %w", name, ctx.Err())
		}
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
