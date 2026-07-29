// Package clientinject provides the openfsd Client Setup engine: version-pinned
// profiles, adapter plan/apply/revert, transactional .openfsd-bak backups,
// running-process preflight, and Phase 1 hybrid shadow PE launch.
//
// Pure subpackages (stdlib only):
//   - pepatch — PE/binary overwrite helpers
//   - cilus — CLR #US heap encode/decode
//   - vpilotconfig — vPilot 3DES config crypto + XML field rewrite
//
// Hybrid shadow launch (LaunchShadow / --ephemeral): patch a temp copy of the
// PE while cwd remains the install root so DLLs resolve; durable config rewrite
// is the default; install PE stays stock.
//
// This package must not import server, web, afv, db, postoffice, session,
// cluster, sweatbox, metar, auth, or serviceapi.
package clientinject
