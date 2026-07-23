// Package xp12aptdat helps end users fetch X-Plane 12 Global Airports apt.dat
// onto their own machines. openfsd does not redistribute that data.
//
// Legal / packaging background: docs/xplane-airport-data.md
//
// Source (same unsecured CDN tree the official XP12 installer uses for
// non-DRM Global Airports content — no product key / XDD):
//
//  1. GET public lookup server list → SERVER host, /unsecured/ prefix, BRANCH_PATH
//  2. Download …/Global Scenery/Global Airports/Earth nav data/apt.dat.zip
//  3. Extract apt.dat locally
//
// Global Airports / Scenery Gateway layout data is published with a GPLv2
// COPYING notice. Prefer a local X-Plane install or the official Gateway API
// when those are available; this package is a convenience bulk fetch only.
//
// Path segments with spaces must be URL-encoded (%20); raw spaces → CloudFront 403.
// Pure stdlib; no curl/unzip/python.
package xp12aptdat

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultLookupURL is Laminar's public XP12 server list.
	DefaultLookupURL = "https://lookup.x-plane.com/_lookup_12_/server_list_12.txt"
	// DefaultOutDir is the default directory for apt.dat when none is specified.
	DefaultOutDir = "xp12-aptdat"
	// DefaultBranch is the preferred BRANCH label in the server list.
	DefaultBranch = "Final"
	// UserAgent identifies this tooling to the CDN.
	UserAgent = "openfsd-download-xp12-aptdat/1.0"

	aptRelPath  = "Global Scenery/Global Airports/Earth nav data/apt.dat.zip"
	minZipBytes = 1_000_000
)

// Options configures a download of Global Airports apt.dat (or directory.txt.zip).
type Options struct {
	// OutDir is where apt.dat (and optional zip) are written. Default: DefaultOutDir.
	OutDir string
	// AptPath, if set, is the exact destination for the extracted apt.dat.
	// Parent directory is created as needed. Zip is written next to it.
	// When empty, apt.dat is written to OutDir/apt.dat.
	AptPath string
	// KeepZip retains apt.dat.zip after extract.
	KeepZip bool
	// DirectoryOnly downloads directory.txt.zip only (no apt.dat).
	DirectoryOnly bool
	// Branch is the preferred BRANCH label: Final, Beta, or RSG.
	Branch string
	// LookupURL overrides the server-list URL.
	LookupURL string
	// Log receives status lines (default: os.Stdout). Set to io.Discard to silence.
	Log io.Writer
	// Progress receives download progress on stderr-style updates (default: os.Stderr).
	Progress io.Writer
}

// Result is the outcome of a successful Download.
type Result struct {
	AptPath string // empty when DirectoryOnly
	ZipPath string // empty when zip removed or DirectoryOnly with no apt zip
	Host    string
	Prefix  string
	Branch  string
	BaseURL string
}

// Download fetches (and usually extracts) XP12 Global Airports data.
func Download(opts Options) (*Result, error) {
	if opts.OutDir == "" {
		opts.OutDir = DefaultOutDir
	}
	if opts.Branch == "" {
		opts.Branch = DefaultBranch
	}
	if opts.LookupURL == "" {
		opts.LookupURL = DefaultLookupURL
	}
	log := opts.Log
	if log == nil {
		log = os.Stdout
	}
	progress := opts.Progress
	if progress == nil {
		progress = os.Stderr
	}

	printDataNotice(log)
	fmt.Fprintf(log, "==> Fetching server list: %s\n", opts.LookupURL)
	listBody, err := httpGetBytes(opts.LookupURL, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("server list: %w", err)
	}
	host, prefix, branch, err := ParseServerList(string(listBody), opts.Branch)
	if err != nil {
		return nil, err
	}

	base, err := JoinCDNBase(host, prefix, branch)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(log, "    CDN host:   %s\n", host)
	fmt.Fprintf(log, "    prefix:     %s\n", prefix)
	fmt.Fprintf(log, "    branch:     %s\n", branch)
	fmt.Fprintf(log, "    base URL:   %s\n", base)

	res := &Result{Host: host, Prefix: prefix, Branch: branch, BaseURL: base}

	if opts.DirectoryOnly {
		if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
			return nil, err
		}
		dest := filepath.Join(opts.OutDir, "directory.txt.zip")
		u, err := JoinURL(base, "directory.txt.zip")
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(log, "==> Downloading directory.txt.zip\n    %s\n", u)
		if err := downloadFile(u, dest, 5*time.Minute, progress); err != nil {
			return nil, err
		}
		fmt.Fprintf(log, "    -> %s (%s)\n", dest, humanSize(fileSize(dest)))
		if err := listZip(log, dest, 10); err != nil {
			return nil, err
		}
		res.ZipPath = dest
		return res, nil
	}

	aptPath := opts.AptPath
	if aptPath == "" {
		aptPath = filepath.Join(opts.OutDir, "apt.dat")
	}
	outDir := filepath.Dir(aptPath)
	if outDir == "" || outDir == "." {
		// keep as-is
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	zipPath := filepath.Join(outDir, "apt.dat.zip")
	u, err := JoinURL(base, aptRelPath)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(log, "==> Downloading Global Airports apt.dat.zip\n    %s\n", u)
	if err := downloadFile(u, zipPath, 30*time.Minute, progress); err != nil {
		return nil, err
	}
	sz := fileSize(zipPath)
	if sz < minZipBytes {
		preview, _ := os.ReadFile(zipPath)
		if len(preview) > 400 {
			preview = preview[:400]
		}
		return nil, fmt.Errorf("download too small (%d bytes); body: %q", sz, preview)
	}
	fmt.Fprintf(log, "    zip size: %s\n", humanSize(sz))

	fmt.Fprintln(log, "==> Extracting apt.dat")
	if err := extractNamedMember(zipPath, "apt.dat", aptPath); err != nil {
		if err2 := extractFirstNamed(zipPath, "apt.dat", aptPath); err2 != nil {
			return nil, fmt.Errorf("extract apt.dat: %v (fallback: %v)", err, err2)
		}
	}

	if !opts.KeepZip {
		_ = os.Remove(zipPath)
		zipPath = ""
	}

	fmt.Fprintln(log, "==> Done")
	fmt.Fprintf(log, "    %s (%s)\n", aptPath, humanSize(fileSize(aptPath)))
	header, airports, err := SummarizeAPT(aptPath)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(log, "    header: %s\n", header)
	fmt.Fprintf(log, "    land airport headers (^1 ): %d\n", airports)

	res.AptPath = aptPath
	res.ZipPath = zipPath
	return res, nil
}

// ParseServerList extracts CDN host, URL prefix, and BRANCH_PATH from a server list body.
func ParseServerList(body, branchPref string) (host, prefix, branch string, err error) {
	lines := splitLines(body)
	for i, line := range lines {
		if strings.HasPrefix(line, "SERVER ") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return "", "", "", fmt.Errorf("malformed SERVER line: %q", line)
			}
			host = fields[1]
			if i+1 < len(lines) {
				prefix = strings.TrimSpace(lines[i+1])
			}
			break
		}
	}
	if host == "" || prefix == "" {
		return "", "", "", fmt.Errorf("could not parse SERVER host/prefix from server list")
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	want := strings.EqualFold
	pref := strings.TrimSpace(branchPref)
	var firstPath string
	active := false
	for _, line := range lines {
		if strings.HasPrefix(line, "BRANCH ") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "BRANCH "))
			name = strings.Fields(name)[0]
			active = want(name, pref)
			continue
		}
		if strings.HasPrefix(line, "BRANCH_PATH ") {
			p := strings.TrimSpace(strings.TrimPrefix(line, "BRANCH_PATH "))
			if firstPath == "" {
				firstPath = p
			}
			if active {
				branch = p
				break
			}
		}
	}
	if branch == "" {
		branch = firstPath
	}
	if branch == "" {
		return "", "", "", fmt.Errorf("no BRANCH_PATH in server list")
	}
	return host, prefix, branch, nil
}

// JoinCDNBase builds https://{host}{prefix}{encoded-branch} without double-escaping.
func JoinCDNBase(host, prefix, branch string) (string, error) {
	basePath := strings.TrimSuffix(prefix, "/") + "/" + EncodePathKeepSlash(branch)
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return "https://" + host + basePath, nil
}

// JoinURL appends a relative path (with spaces) to a CDN base URL.
func JoinURL(base, rel string) (string, error) {
	enc := EncodePathKeepSlash(rel)
	if strings.HasSuffix(base, "/") {
		return base + enc, nil
	}
	return base + "/" + enc, nil
}

// EncodePathKeepSlash percent-encodes each path segment (spaces → %20) but keeps '/'.
func EncodePathKeepSlash(p string) string {
	p = strings.TrimPrefix(p, "/")
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}

// SummarizeAPT returns a short header line and land-airport count for an apt.dat file.
func SummarizeAPT(path string) (header string, airports int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	sc.Buffer(buf, 8*1024*1024)
	var hdr []string
	for sc.Scan() {
		line := sc.Text()
		if len(hdr) < 2 {
			hdr = append(hdr, strings.TrimSpace(line))
		}
		if strings.HasPrefix(line, "1 ") {
			airports++
		}
	}
	if err := sc.Err(); err != nil {
		return "", 0, err
	}
	return strings.Join(hdr, " "), airports, nil
}

// printDataNotice reminds operators that openfsd is not redistributing Laminar data.
func printDataNotice(w io.Writer) {
	fmt.Fprintln(w, "Note: downloading X-Plane Global Airports apt.dat to YOUR machine only.")
	fmt.Fprintln(w, "      openfsd does not host or redistribute this file. Layout data is")
	fmt.Fprintln(w, "      from Laminar Research / Scenery Gateway (see Global Airports COPYING,")
	fmt.Fprintln(w, "      typically GPLv2). Details: docs/xplane-airport-data.md")
	fmt.Fprintln(w)
}

func httpGetBytes(rawURL string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, truncate(string(body), 200))
	}
	return io.ReadAll(resp.Body)
}

func downloadFile(rawURL, dest string, timeout time.Duration, progress io.Writer) error {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %s for %s: %s", resp.Status, rawURL, truncate(string(body), 200))
	}

	tmp := dest + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
	}()

	var written int64
	total := resp.ContentLength
	buf := make([]byte, 256*1024)
	lastPrint := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				_ = os.Remove(tmp)
				return werr
			}
			written += int64(n)
			if progress != nil && time.Since(lastPrint) > 500*time.Millisecond {
				printProgress(progress, written, total)
				lastPrint = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = os.Remove(tmp)
			return readErr
		}
	}
	if progress != nil {
		printProgress(progress, written, total)
		fmt.Fprintln(progress)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func printProgress(w io.Writer, written, total int64) {
	if total > 0 {
		pct := 100 * float64(written) / float64(total)
		fmt.Fprintf(w, "\r    %6.1f%%  %s / %s", pct, humanSize(written), humanSize(total))
	} else {
		fmt.Fprintf(w, "\r    %s downloaded", humanSize(written))
	}
}

func extractNamedMember(zipPath, member, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := f.Name
		if name == member || path.Base(name) == member && !strings.Contains(name, "..") {
			return unzipFile(f, dest)
		}
	}
	return fmt.Errorf("member %q not found", member)
}

func extractFirstNamed(zipPath, baseName, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if path.Base(f.Name) == baseName && !f.FileInfo().IsDir() {
			return unzipFile(f, dest)
		}
	}
	return fmt.Errorf("no member basenamed %q", baseName)
}

func unzipFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func listZip(w io.Writer, zipPath string, max int) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	fmt.Fprintf(w, "    archive has %d file(s):\n", len(r.File))
	for i, f := range r.File {
		if i >= max {
			fmt.Fprintf(w, "    ... +%d more\n", len(r.File)-max)
			break
		}
		fmt.Fprintf(w, "    %12d  %s\n", f.UncompressedSize64, f.Name)
	}
	return nil
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
