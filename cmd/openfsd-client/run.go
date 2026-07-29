package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/adapters"
	"github.com/renorris/openfsd/internal/clientinject/adapters/vpilot"
)

// version is set at link time by packaging (-ldflags "-X main.version=…").
var version = "dev"

// Exit codes (design: docs/design/client-runtime-injector.md).
const (
	ExitOK            = 0
	ExitUsage         = 1
	ExitHashMismatch  = 2
	ExitApplyFailed   = 3
	ExitRevertFailed  = 4
	ExitClientRunning = 5
	ExitPlanBlockers  = 6
	// ExitClientProcess: client binary started but exited non-zero (ephemeral launch).
	ExitClientProcess = 7
)

// usageText is built at runtime so the linked version appears in help.
func usageText() string {
	return `openfsd-client — openfsd Client Setup (GUI + headless CLI)
Version: ` + version + `

Usage:
  openfsd-client                          Launch GUI when a display is available; else this help
  openfsd-client list-profiles
  openfsd-client detect --client vpilot
  openfsd-client plan|apply|revert|health|launch [flags]

Shared flags for plan|apply|health|launch:
  --web-base URL          openfsd web origin (e.g. https://fsd.ex.co)
  --fsd-host HOST         FSD hostname
  --fsd-port PORT         FSD TCP port (default 6809, omitted from server list when default)
  --fsd-server-name NAME  CachedServers label (default OPENFSD)
  --afv-base URL          AFV REST public base (optional)
  --force-disable-afv     Always launch with -novoice (vPilot)
  --prefer-short-jwt      Prefer short JWT paths (/j, /fsd-jwt, …) for #US budget (vPilot)
  --install DIR           Client install directory (required when not auto-detected)
  --client ID             Client adapter id (default vpilot; Windows vPilot only today)
  --profiles-dir DIR      Optional override profile directory (YAML)

Launch-only flags:
  --ephemeral             Phase 1 hybrid shadow PE: patch a temp PE copy, cwd=install
                          root (DLLs resolve from install); durable config rewrite
                          remains default (with .openfsd-bak; restored only if
                          apply fails). Install PE stays stock. Nested DLL shadow
                          preserves relpath under temp (Phase 1 typically PE-only).

Readiness honesty:
  vPilot: default JWT path /api/v1/fsd-jwt allows max host 12 characters for
  in-place #US patch (budget 35 runes). Longer hosts use free-slot #US remap
  (large cosmetic string sacrificed). Use --prefer-short-jwt for /j (max host
  25 in-place) if the server has fixed short JWT routes (direct POST /j, not a 302).
  AFV PE ret-disable is out of scope until research gate R2; over-budget AFV
  remaps when free slots remain, else -novoice.

Exit codes:
  0 ok
  1 usage
  2 hash mismatch (unknown / wrong PE)
  3 apply failed (auto-reverted when possible)
  4 revert failed
  5 client running / file locked
  6 plan blockers
  7 client process exited non-zero (ephemeral launch only)
`
}

// Run parses args and executes a subcommand. stdout/stderr are injectable for tests.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usageText())
		return ExitOK
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText())
		return ExitOK
	case "list-profiles":
		return cmdListProfiles(rest, stdout, stderr)
	case "detect":
		return cmdDetect(rest, stdout, stderr)
	case "plan":
		return cmdPlan(rest, stdout, stderr)
	case "apply":
		return cmdApply(rest, stdout, stderr)
	case "revert":
		return cmdRevert(rest, stdout, stderr)
	case "health":
		return cmdHealth(rest, stdout, stderr)
	case "launch":
		return cmdLaunch(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n\n", cmd)
		fmt.Fprint(stderr, usageText())
		return ExitUsage
	}
}

func cmdListProfiles(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list-profiles", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profilesDir := fs.String("profiles-dir", "", "optional profiles directory")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	store, err := loadProfiles(*profilesDir)
	if err != nil {
		fmt.Fprintf(stderr, "load profiles: %v\n", err)
		return ExitUsage
	}
	for _, p := range store.List() {
		fmt.Fprintf(stdout, "%s\t%s\t%s\tsha1=%s\n",
			p.ProfileID, p.ClientID, p.SupportedClientVersion, p.PrimaryBinary.SHA1)
	}
	return ExitOK
}

func cmdDetect(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	client := fs.String("client", "vpilot", "client id")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	eng, err := newEngine("")
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitUsage
	}
	a, ok := eng.Adapters[*client]
	if !ok {
		fmt.Fprintf(stderr, "unknown client %q\n", *client)
		return ExitUsage
	}
	cands, err := a.Discover(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "detect: %v\n", err)
		return ExitUsage
	}
	if len(cands) == 0 {
		fmt.Fprintf(stdout, "no %s installs found (pass --install DIR)\n", *client)
		return ExitOK
	}
	for _, c := range cands {
		fmt.Fprintf(stdout, "root=%s pe=%s configs=%v hint=%s\n",
			c.RootDir, c.PrimaryPE, c.ConfigPaths, c.DisplayHint)
	}
	return ExitOK
}

type endpointFlags struct {
	webBase         string
	fsdHost         string
	fsdPort         int
	fsdServerName   string
	afvBase         string
	forceDisableAFV bool
	preferShortJWT  bool
	install         string
	client          string
	profilesDir     string
	ephemeral       bool // launch --ephemeral (hybrid shadow PE)
}

func parseEndpointFlags(name string, args []string, stderr io.Writer) (*endpointFlags, []string, int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	ef := &endpointFlags{}
	fs.StringVar(&ef.webBase, "web-base", "", "openfsd web base URL")
	fs.StringVar(&ef.fsdHost, "fsd-host", "", "FSD hostname")
	fs.IntVar(&ef.fsdPort, "fsd-port", 0, "FSD port (0 = default 6809)")
	fs.StringVar(&ef.fsdServerName, "fsd-server-name", "OPENFSD", "cached server name")
	fs.StringVar(&ef.afvBase, "afv-base", "", "AFV base URL")
	fs.BoolVar(&ef.forceDisableAFV, "force-disable-afv", false, "force -novoice")
	fs.BoolVar(&ef.preferShortJWT, "prefer-short-jwt", false, "prefer short JWT path templates")
	fs.StringVar(&ef.install, "install", "", "client install directory")
	fs.StringVar(&ef.client, "client", "vpilot", "client id")
	fs.StringVar(&ef.profilesDir, "profiles-dir", "", "optional profiles directory")
	// Launch-only; harmless no-op if passed to other subcommands.
	fs.BoolVar(&ef.ephemeral, "ephemeral", false, "hybrid shadow PE launch (temp PE, cwd=install, durable config)")
	if err := fs.Parse(args); err != nil {
		return nil, nil, ExitUsage
	}
	return ef, fs.Args(), ExitOK
}

func (ef *endpointFlags) endpoints() clientinject.Endpoints {
	return clientinject.Endpoints{
		WebBaseURL:         ef.webBase,
		FSDHost:            ef.fsdHost,
		FSDPort:            ef.fsdPort,
		FSDServerName:      ef.fsdServerName,
		AFVBaseURL:         ef.afvBase,
		ForceDisableAFV:    ef.forceDisableAFV,
		PreferShortJWTPath: ef.preferShortJWT,
	}
}

func cmdPlan(args []string, stdout, stderr io.Writer) int {
	ef, _, code := parseEndpointFlags("plan", args, stderr)
	if code != ExitOK {
		return code
	}
	return withInstallPlan(ef, stdout, stderr, func(eng *clientinject.Engine, install clientinject.Install, ep clientinject.Endpoints) int {
		plan, err := eng.Plan(context.Background(), install, ep)
		if err != nil {
			return mapPlanErr(err, stderr)
		}
		printPlan(stdout, plan)
		if len(plan.Blockers) > 0 {
			return ExitPlanBlockers
		}
		return ExitOK
	})
}

func cmdApply(args []string, stdout, stderr io.Writer) int {
	ef, _, code := parseEndpointFlags("apply", args, stderr)
	if code != ExitOK {
		return code
	}
	return withInstallPlan(ef, stdout, stderr, func(eng *clientinject.Engine, install clientinject.Install, ep clientinject.Endpoints) int {
		plan, err := eng.Plan(context.Background(), install, ep)
		if err != nil {
			return mapPlanErr(err, stderr)
		}
		printPlan(stdout, plan)
		if len(plan.Blockers) > 0 {
			fmt.Fprintf(stderr, "plan has blockers; refuse Apply\n")
			return ExitPlanBlockers
		}
		res, err := eng.Apply(context.Background(), plan)
		if err != nil {
			return mapApplyErr(err, stderr)
		}
		fmt.Fprintf(stdout, "applied ok manifest=%s applied=%v\n", res.ManifestPath, res.Applied)
		return ExitOK
	})
}

func cmdRevert(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("revert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	install := fs.String("install", "", "client install directory")
	profilesDir := fs.String("profiles-dir", "", "optional profiles directory")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *install == "" {
		fmt.Fprintf(stderr, "--install is required for revert\n")
		return ExitUsage
	}
	eng, err := newEngine(*profilesDir)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitUsage
	}
	if err := eng.Revert(context.Background(), filepath.Clean(*install)); err != nil {
		fmt.Fprintf(stderr, "revert: %v\n", err)
		return ExitRevertFailed
	}
	fmt.Fprintf(stdout, "reverted %s\n", *install)
	return ExitOK
}

func cmdHealth(args []string, stdout, stderr io.Writer) int {
	ef, _, code := parseEndpointFlags("health", args, stderr)
	if code != ExitOK {
		return code
	}
	return withInstallPlan(ef, stdout, stderr, func(eng *clientinject.Engine, install clientinject.Install, ep clientinject.Endpoints) int {
		a, ok := eng.Adapters[install.ClientID]
		if !ok {
			fmt.Fprintf(stderr, "no adapter for %q\n", install.ClientID)
			return ExitUsage
		}
		// Fill profile id for healthcheck.
		if p, err := eng.ResolveProfile(install); err == nil {
			install.ProfileID = p.ProfileID
			if install.HashSHA1 == "" {
				install.HashSHA1 = p.PrimaryBinary.SHA1
			}
		}
		if err := a.HealthCheck(install, ep); err != nil {
			fmt.Fprintf(stderr, "health: %v\n", err)
			return ExitApplyFailed
		}
		fmt.Fprintf(stdout, "health ok\n")
		return ExitOK
	})
}

func cmdLaunch(args []string, stdout, stderr io.Writer) int {
	ef, _, code := parseEndpointFlags("launch", args, stderr)
	if code != ExitOK {
		return code
	}
	return withInstallPlan(ef, stdout, stderr, func(eng *clientinject.Engine, install clientinject.Install, ep clientinject.Endpoints) int {
		a, ok := eng.Adapters[install.ClientID]
		if !ok {
			fmt.Fprintf(stderr, "no adapter for %q\n", install.ClientID)
			return ExitUsage
		}
		if !ef.ephemeral {
			launchArgs := a.LaunchArgs(install, ep)
			pe := clientinject.AbsPrimaryPE(install)
			fmt.Fprintf(stdout, "exec: %s %s\n", pe, strings.Join(launchArgs, " "))
			fmt.Fprintf(stdout, "(launch is dry-print only without --ephemeral; use --ephemeral for Phase 1 hybrid shadow PE)\n")
			return ExitOK
		}

		// Phase 1 hybrid shadow PE launch (Appendix B).
		// Cancel on SIGINT/SIGTERM so the child is killed (CommandContext) and
		// temp shadow dirs are still cleaned (best-effort; SIGKILL can orphan).
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		plan, err := eng.Plan(ctx, install, ep)
		if err != nil {
			return mapPlanErr(err, stderr)
		}
		printPlan(stdout, plan)
		if len(plan.Blockers) > 0 {
			fmt.Fprintf(stderr, "plan has blockers; refuse ephemeral launch\n")
			return ExitPlanBlockers
		}
		launchArgs := a.LaunchArgs(install, ep)
		// Prefer plan launch_flag args when present.
		if fromPlan := clientinject.LaunchArgsFromPlan(plan); len(fromPlan) > 0 {
			launchArgs = fromPlan
		}
		fmt.Fprintf(stdout, "ephemeral shadow launch: cwd=%s pe=temp-copy durable_config=true (bak+restore on apply fail)\n", install.RootDir)
		fmt.Fprintf(stdout, "note: successful launch leaves install config rewritten; install PE stays stock\n")
		fmt.Fprintf(stdout, "launch args: %s\n", strings.Join(launchArgs, " "))
		session, err := eng.LaunchShadow(ctx, plan, launchArgs, clientinject.ShadowLaunchConfig{})
		if err != nil {
			return mapLaunchErr(err, stderr)
		}
		if session != nil {
			fmt.Fprintf(stdout, "ephemeral launch finished (temp_kept=%v exe=%s install_pe_stock_fp=%s)\n",
				session.KeptTemp(), session.Exe, session.InstallPEFingerprint)
		}
		return ExitOK
	})
}

func mapLaunchErr(err error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "ephemeral launch: %v\n", err)
	if clientinject.IsProcessExit(err) {
		return ExitClientProcess
	}
	if errors.Is(err, clientinject.ErrClientRunning) || strings.Contains(strings.ToLower(err.Error()), "running") || strings.Contains(strings.ToLower(err.Error()), "locked") {
		return ExitClientRunning
	}
	if strings.Contains(err.Error(), "blockers") {
		return ExitPlanBlockers
	}
	if strings.Contains(err.Error(), "sha1") || strings.Contains(err.Error(), "hash") {
		return ExitHashMismatch
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// Canceled after prepare: treat as apply/launch aborted (not client exit).
		return ExitApplyFailed
	}
	return ExitApplyFailed
}

func withInstallPlan(ef *endpointFlags, stdout, stderr io.Writer, fn func(*clientinject.Engine, clientinject.Install, clientinject.Endpoints) int) int {
	_ = stdout
	if ef.install == "" {
		fmt.Fprintf(stderr, "--install DIR is required\n")
		return ExitUsage
	}
	if ef.client == "" {
		ef.client = "vpilot"
	}
	eng, err := newEngine(ef.profilesDir)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return ExitUsage
	}
	if _, ok := eng.Adapters[ef.client]; !ok {
		fmt.Fprintf(stderr, "unknown client %q\n", ef.client)
		return ExitUsage
	}
	install := buildInstall(eng, ef.client, ef.install)
	ep := ef.endpoints()
	return fn(eng, install, ep)
}

// buildInstall constructs an Install and seeds ProfileID/HashSHA1 from a prior
// inject manifest when present so re-plan/re-apply works after the live PE is patched.
func buildInstall(eng *clientinject.Engine, clientID, root string) clientinject.Install {
	root = filepath.Clean(root)
	var install clientinject.Install
	switch clientID {
	case "vpilot":
		install = vpilot.InstallFromDir(root)
	default:
		// Fall back to profile primary binary name when available.
		primary := "client.exe"
		if eng != nil && eng.Profiles != nil {
			for _, p := range eng.Profiles.ForClient(clientID) {
				if p.PrimaryBinary.RelativePath != "" {
					primary = p.PrimaryBinary.RelativePath
					break
				}
			}
		}
		install = clientinject.Install{
			ClientID:  clientID,
			RootDir:   root,
			PrimaryPE: filepath.Join(root, primary),
		}
	}
	seedInstallFromManifest(eng, &install)
	return install
}

func seedInstallFromManifest(eng *clientinject.Engine, install *clientinject.Install) {
	if install == nil || install.RootDir == "" {
		return
	}
	var writer clientinject.FileWriter = clientinject.OSFileWriter{}
	if eng != nil && eng.Writer != nil {
		writer = eng.Writer
	}
	m, err := clientinject.ReadManifest(writer, install.RootDir)
	if err != nil {
		return
	}
	if install.ProfileID == "" && m.ProfileID != "" {
		install.ProfileID = m.ProfileID
	}
	if install.HashSHA1 == "" && m.PESHA1 != "" {
		install.HashSHA1 = m.PESHA1
	}
}

func loadProfiles(dir string) (*clientinject.ProfileStore, error) {
	if dir != "" {
		return clientinject.LoadFromDir(dir)
	}
	return clientinject.LoadEmbedded()
}

func newEngine(profilesDir string) (*clientinject.Engine, error) {
	store, err := loadProfiles(profilesDir)
	if err != nil {
		return nil, fmt.Errorf("load profiles: %w", err)
	}
	return clientinject.NewEngine(store, adapters.DefaultAdapters()...), nil
}

func printPlan(w io.Writer, plan *clientinject.Plan) {
	fmt.Fprintf(w, "profile=%s client=%s pe=%s\n", plan.Install.ProfileID, plan.Install.ClientID, plan.Install.PrimaryPE)
	fmt.Fprintf(w, "endpoints: web=%s fsd=%s afv=%s short_jwt=%v force_novoice=%v\n",
		plan.Endpoints.WebBaseURL, plan.Endpoints.FSDAddress(), plan.Endpoints.AFVBaseURL,
		plan.Endpoints.PreferShortJWTPath, plan.Endpoints.ForceDisableAFV)
	for _, c := range plan.Constraints {
		fmt.Fprintf(w, "constraint: field=%s max_runes=%d strategy=%s — %s\n",
			c.Field, c.MaxRunes, c.Strategy, c.Description)
	}
	for _, m := range plan.Mutations {
		fmt.Fprintf(w, "mutation: id=%s kind=%s — %s\n", m.ID, m.Kind, m.Description)
	}
	for _, warn := range plan.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warn)
	}
	for _, b := range plan.Blockers {
		fmt.Fprintf(w, "blocker: %s\n", b)
	}
	if len(plan.Blockers) == 0 {
		fmt.Fprintf(w, "plan: OK (%d mutations)\n", len(plan.Mutations))
	} else {
		fmt.Fprintf(w, "plan: BLOCKED (%d blockers)\n", len(plan.Blockers))
	}
}

func mapPlanErr(err error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "plan: %v\n", err)
	msg := err.Error()
	if strings.Contains(msg, "sha1") || strings.Contains(msg, "hash") || strings.Contains(msg, "unknown hash") {
		return ExitHashMismatch
	}
	if errors.Is(err, clientinject.ErrClientRunning) || strings.Contains(msg, "running") {
		return ExitClientRunning
	}
	return ExitApplyFailed
}

func mapApplyErr(err error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "apply: %v\n", err)
	msg := err.Error()
	if errors.Is(err, clientinject.ErrClientRunning) || strings.Contains(strings.ToLower(msg), "running") || strings.Contains(msg, "locked") {
		return ExitClientRunning
	}
	if strings.Contains(msg, "sha1") || strings.Contains(msg, "does not match profile") {
		return ExitHashMismatch
	}
	if strings.Contains(msg, "blockers") {
		return ExitPlanBlockers
	}
	return ExitApplyFailed
}
