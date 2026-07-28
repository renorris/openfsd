package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RqliteClient is a minimal HTTP client for rqlite execute/query.
// Supports strong/weak/none read levels via query string level=.
type RqliteClient struct {
	base    string // e.g. http://127.0.0.1:4001
	http    *http.Client
	timeout time.Duration
}

// RqliteClientOptions configures the client.
type RqliteClientOptions struct {
	Timeout    time.Duration
	HTTPClient *http.Client
}

// NewRqliteClient creates a client for the given base URL (no trailing slash required).
func NewRqliteClient(baseURL string, opts RqliteClientOptions) *RqliteClient {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &RqliteClient{base: base, http: hc, timeout: timeout}
}

// BaseURL returns the configured base URL.
func (c *RqliteClient) BaseURL() string { return c.base }

// Ping checks status endpoint (or query SELECT 1).
func (c *RqliteClient) Ping(ctx context.Context) error {
	_, err := c.Query(ctx, ReadNone, "SELECT 1")
	return err
}

type rqliteRequest [][]any

type rqliteResponse struct {
	Results []rqliteResult `json:"results"`
	Error   string         `json:"error"`
}

type rqliteResult struct {
	Columns      []string `json:"columns"`
	Types        []string `json:"types"`
	Values       [][]any  `json:"values"`
	Error        string   `json:"error"`
	RowsAffected int64    `json:"rows_affected"`
	LastInsertID int64    `json:"last_insert_id"`
}

// Query runs a read statement at the given consistency level.
// stmts are parameterised: first element SQL, rest are args (rqlite associative form).
func (c *RqliteClient) Query(ctx context.Context, level ReadLevel, sqlStmt string, args ...any) (*rqliteResult, error) {
	body := rqliteRequest{append([]any{sqlStmt}, args...)}
	u := c.base + "/db/query?timings&level=" + url.QueryEscape(string(level))
	return c.do(ctx, http.MethodPost, u, body)
}

// Execute runs a write (Raft) statement.
func (c *RqliteClient) Execute(ctx context.Context, sqlStmt string, args ...any) (*rqliteResult, error) {
	body := rqliteRequest{append([]any{sqlStmt}, args...)}
	u := c.base + "/db/execute?timings"
	return c.do(ctx, http.MethodPost, u, body)
}

// ExecuteMulti runs multiple write statements in one request.
func (c *RqliteClient) ExecuteMulti(ctx context.Context, stmts [][]any) ([]rqliteResult, error) {
	body := rqliteRequest(stmts)
	u := c.base + "/db/execute?timings"
	raw, err := c.doRaw(ctx, http.MethodPost, u, body)
	if err != nil {
		return nil, err
	}
	var resp rqliteResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("rqlite decode: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("rqlite: %s", resp.Error)
	}
	return resp.Results, nil
}

func (c *RqliteClient) do(ctx context.Context, method, u string, body any) (*rqliteResult, error) {
	raw, err := c.doRaw(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	var resp rqliteResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("rqlite decode: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("rqlite: %s", resp.Error)
	}
	if len(resp.Results) == 0 {
		return &rqliteResult{}, nil
	}
	r := &resp.Results[0]
	if r.Error != "" {
		return nil, fmt.Errorf("rqlite result: %s", r.Error)
	}
	return r, nil
}

func (c *RqliteClient) doRaw(ctx context.Context, method, u string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// Follow leader redirect only to same host family (never open redirect).
	if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound {
		loc := resp.Header.Get("Location")
		if loc != "" && sameOriginOrRelative(c.base, loc) {
			if !strings.HasPrefix(loc, "http") {
				loc = strings.TrimRight(c.base, "/") + loc
			}
			return c.doRaw(ctx, method, loc, body)
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rqlite HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	return raw, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// sameOriginOrRelative allows leader redirects only within the configured base host.
func sameOriginOrRelative(base, loc string) bool {
	if strings.HasPrefix(loc, "/") {
		return true
	}
	// Parse hosts naively
	baseHost := hostFromHTTPURL(base)
	locHost := hostFromHTTPURL(loc)
	return baseHost != "" && baseHost == locHost
}

func hostFromHTTPURL(raw string) string {
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// scanString returns the first column of the first row as string.
func (r *rqliteResult) scanString() (string, bool) {
	if r == nil || len(r.Values) == 0 || len(r.Values[0]) == 0 {
		return "", false
	}
	return anyToString(r.Values[0][0]), true
}

func anyToString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// JSON numbers
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

func anyToInt(v any) int {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var i int
		_, _ = fmt.Sscanf(t, "%d", &i)
		return i
	default:
		return 0
	}
}

func anyToStringPtr(v any) *string {
	if v == nil {
		return nil
	}
	s := anyToString(v)
	return &s
}
