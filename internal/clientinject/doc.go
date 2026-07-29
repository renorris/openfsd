// Package clientinject provides the openfsd Client Setup engine: version-pinned
// profiles, adapter plan/apply/revert, transactional .openfsd-bak backups, and
// running-process preflight for on-disk client reconfiguration.
//
// Pure subpackages (stdlib only):
//   - pepatch — PE/binary overwrite helpers
//   - cilus / vpilotconfig — land in sibling PRs
//
// This package must not import server, web, afv, db, postoffice, session,
// cluster, sweatbox, metar, auth, or serviceapi.
package clientinject
