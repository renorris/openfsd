package gui

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/renorris/openfsd/internal/clientinject"
)

// BuildInstall constructs a client-agnostic Install for rootDir.
// Uses adapter Discover when the path matches a candidate; otherwise fills
// PrimaryPE from the first matching profile's relative path. Seeds ProfileID /
// HashSHA1 from an existing inject manifest when present.
//
// No client_id switch for layout — only Adapter + ProfileStore data.
func BuildInstall(eng *clientinject.Engine, clientID, rootDir string) clientinject.Install {
	rootDir = filepath.Clean(strings.TrimSpace(rootDir))
	install := clientinject.Install{
		ClientID: clientID,
		RootDir:  rootDir,
	}
	if eng == nil || rootDir == "" || rootDir == "." {
		return install
	}

	if a, ok := eng.Adapters[clientID]; ok && a != nil {
		if cands, err := a.Discover(context.Background()); err == nil {
			for _, c := range cands {
				if filepath.Clean(c.RootDir) == rootDir {
					install.PrimaryPE = c.PrimaryPE
					install.ConfigPaths = append([]string(nil), c.ConfigPaths...)
					break
				}
			}
		}
	}

	if install.PrimaryPE == "" && eng.Profiles != nil {
		for _, p := range eng.Profiles.List() {
			if p.ClientID != clientID {
				continue
			}
			if p.PrimaryBinary.RelativePath != "" {
				install.PrimaryPE = filepath.Join(rootDir, p.PrimaryBinary.RelativePath)
			}
			// Config candidates from profile relative paths that exist.
			w := eng.Writer
			if w == nil {
				w = clientinject.OSFileWriter{}
			}
			for _, cf := range p.ConfigFiles {
				if cf.RelativePath == "" {
					continue
				}
				cp := filepath.Join(rootDir, cf.RelativePath)
				if _, err := w.Stat(cp); err == nil {
					install.ConfigPaths = appendUnique(install.ConfigPaths, cp)
				}
			}
			break
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

func appendUnique(slice []string, v string) []string {
	v = filepath.Clean(v)
	for _, s := range slice {
		if filepath.Clean(s) == v {
			return slice
		}
	}
	return append(slice, v)
}

// FirstDetectPath returns the first Discover candidate root for clientID, or "".
func FirstDetectPath(eng *clientinject.Engine, clientID string) (clientinject.InstallCandidate, bool) {
	if eng == nil {
		return clientinject.InstallCandidate{}, false
	}
	a, ok := eng.Adapters[clientID]
	if !ok || a == nil {
		return clientinject.InstallCandidate{}, false
	}
	cands, err := a.Discover(context.Background())
	if err != nil || len(cands) == 0 {
		return clientinject.InstallCandidate{}, false
	}
	return cands[0], true
}
