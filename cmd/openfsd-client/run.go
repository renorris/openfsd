package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/adapters"
	"github.com/renorris/openfsd/internal/clientinject/adapters/vpilot"
)

// Exit codes (design: docs/design/client-runtime-injector.md).
const (
	ExitOK            = 0
	ExitUsage         = 1
	ExitHashMismatch  = 2
	ExitApplyFailed   = 3
	ExitRevertFailed  = 4
	ExitClientRunning = 5
	ExitPlanBlockers  = 6
)

const usageText = `openfsd-client — openfsd Client Setup (headless CLI)

Usage:
  openfsd-client                          Print this help (GUI lands in a later PR)
  openfsd-client list-profiles
  openfsd-client detect --client vpilot
  openfsd-client plan|apply|revert|health|launch [flags]

Shared flags for plan|apply|health|launch:
  --web-base URL          openfsd web origin (e.g. https://fsd.ex.co)
  --fsd-host HOST         FSD hostname
  --fsd-port PORT         FSD TCP port (default 6809, omitted from server list when default)
  --fsd-server-name NAME  CachedServers label (default OPENFSD)
  --afv-base URL          AFV REST public base (optional)
  --force-disable-afv     Always launch with -novoice
  --prefer-short-jwt      Prefer short JWT paths (/j, /fsd-jwt, …) for #US budget
  --install DIR           vPilot install directory (required when not auto-detected)
  --client ID             Client adapter id (default vpilot)
  --profiles-dir DIR      Optional override profile directory (YAML)

Readiness honesty:
  Default JWT path /api/v1/fsd-jwt allows max host 12 characters for in-place #US
  patch (budget 35 runes). Use --prefer-short-jwt for /j (max host 25) if the
  server has fixed short JWT routes (direct POST /j, not a 302 redirect).
  Long hostnames without short paths or free-slot remap are plan blockers.
  AFV PE ret-disable is out of scope until research gate R2; over-budget AFV
  falls back to -novoice.

Exit codes:
  0 ok
  1 usage
  2 hash mismatch (unknown / wrong PE)
  3 apply failed (auto-reverted when possible)
  4 revert failed
  5 client running / file locked
  6 plan blockers
`

// Run parses args and executes a subcommand. stdout/stderr are injectable for tests.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usageText)
		return ExitOK
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
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
		fmt.Fprint(stderr, usageText)
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
		return ExitApplyFailed
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
		launchArgs := a.LaunchArgs(install, ep)
		pe := clientinject.AbsPrimaryPE(install)
		fmt.Fprintf(stdout, "exec: %s %s\n", pe, strings.Join(launchArgs, " "))
		fmt.Fprintf(stdout, "(launch is dry-print only in headless CLI; start the client with these args from the install directory)\n")
		return ExitOK
	})
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
	install := buildInstall(ef.client, ef.install)
	ep := ef.endpoints()
	return fn(eng, install, ep)
}

func buildInstall(clientID, root string) clientinject.Install {
	root = filepath.Clean(root)
	switch clientID {
	case "vpilot":
		return vpilot.InstallFromDir(root)
	default:
		return clientinject.Install{
			ClientID:  clientID,
			RootDir:   root,
			PrimaryPE: filepath.Join(root, "vPilot.exe"),
		}
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
