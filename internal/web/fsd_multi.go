package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/serviceapi"
)

// fsdServiceURLs returns the list of FSD service HTTP bases.
// Entries may be bare URLs or "nodeID=http://host:port".
func (s *Server) fsdServiceURLs() []string {
	raw := strings.TrimSpace(s.cfg.FsdHttpServiceAddresses)
	if raw == "" {
		u := strings.TrimRight(strings.TrimSpace(s.cfg.FsdHttpServiceAddress), "/")
		if u == "" {
			return nil
		}
		return []string{u}
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if i := strings.IndexByte(p, '='); i > 0 && (strings.HasPrefix(p[i+1:], "http://") || strings.HasPrefix(p[i+1:], "https://")) {
			p = p[i+1:]
		}
		p = strings.TrimRight(p, "/")
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		u := strings.TrimRight(strings.TrimSpace(s.cfg.FsdHttpServiceAddress), "/")
		if u != "" {
			return []string{u}
		}
	}
	return out
}

// fsdURLForNode maps CLUSTER_NODE_ID-style id to service URL via
// "id=http://..." entries in FSD_HTTP_SERVICE_ADDRESSES.
func (s *Server) fsdURLForNode(nodeID string) string {
	raw := strings.TrimSpace(s.cfg.FsdHttpServiceAddresses)
	if raw == "" || nodeID == "" {
		return ""
	}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if i := strings.IndexByte(p, '='); i > 0 {
			id := strings.TrimSpace(p[:i])
			url := strings.TrimRight(strings.TrimSpace(p[i+1:]), "/")
			if id == nodeID {
				return url
			}
		}
	}
	return ""
}

// aggregateOnlineUsers fetches /online_users from all FSD service URLs and merges.
func (s *Server) aggregateOnlineUsers(client *http.Client, jwtSecret []byte) (serviceapi.OnlineUsersResponseData, error) {
	urls := s.fsdServiceURLs()
	if len(urls) == 0 {
		return serviceapi.OnlineUsersResponseData{}, fmt.Errorf("no FSD service addresses configured")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	type result struct {
		data serviceapi.OnlineUsersResponseData
		err  error
	}
	ch := make(chan result, len(urls))
	var wg sync.WaitGroup
	for _, base := range urls {
		wg.Add(1)
		go func(base string) {
			defer wg.Done()
			req, err := s.makeFsdHttpServiceHttpRequestTo(base, "GET", "/online_users", nil)
			if err != nil {
				ch <- result{err: err}
				return
			}
			res, err := client.Do(req)
			if err != nil {
				ch <- result{err: err}
				return
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				ch <- result{err: fmt.Errorf("online_users %s: status %d", base, res.StatusCode)}
				return
			}
			var data serviceapi.OnlineUsersResponseData
			if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
				ch <- result{err: err}
				return
			}
			// Tag node_id from URL host if missing
			nodeHint := base
			for i := range data.Pilots {
				if data.Pilots[i].NodeID == "" {
					data.Pilots[i].NodeID = nodeHint
				}
			}
			for i := range data.ATC {
				if data.ATC[i].NodeID == "" {
					data.ATC[i].NodeID = nodeHint
				}
			}
			ch <- result{data: data}
		}(base)
	}
	wg.Wait()
	close(ch)

	merged := serviceapi.OnlineUsersResponseData{
		Pilots: make([]serviceapi.OnlineUserPilot, 0),
		ATC:    make([]serviceapi.OnlineUserATC, 0),
	}
	var firstErr error
	got := 0
	for r := range ch {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		got++
		merged.Pilots = append(merged.Pilots, r.data.Pilots...)
		merged.ATC = append(merged.ATC, r.data.ATC...)
	}
	if got == 0 {
		return merged, firstErr
	}
	return merged, nil
}

// kickUserOnNode POSTs /kick_user to the FSD service for nodeID.
// Mapping: FSD_HTTP_SERVICE_ADDRESSES may use "nodeID=http://host:port" pairs
// (comma-separated). Bare URLs fall back to try-all.
func (s *Server) kickUserOnNode(client *http.Client, callsign, nodeID string) (status int, err error) {
	urls := s.fsdServiceURLs()
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	body, _ := json.Marshal(map[string]string{"callsign": callsign})

	targets := urls
	if nodeID != "" {
		if mapped := s.fsdURLForNode(nodeID); mapped != "" {
			targets = []string{mapped}
		} else {
			// exact URL match only (no substring spoof)
			var filtered []string
			for _, u := range urls {
				if u == nodeID {
					filtered = append(filtered, u)
				}
			}
			if len(filtered) > 0 {
				targets = filtered
			}
			// else try all (best-effort)
		}
	}

	var lastStatus int
	var lastErr error
	for _, base := range targets {
		req, err := s.makeFsdHttpServiceHttpRequestTo(base, "POST", "/kick_user", bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		lastStatus = res.StatusCode
		if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusOK {
			return res.StatusCode, nil
		}
		if res.StatusCode == http.StatusNotFound {
			continue // try next node
		}
		lastErr = fmt.Errorf("kick status %d", res.StatusCode)
	}
	if lastStatus == http.StatusNotFound {
		return http.StatusNotFound, nil
	}
	return lastStatus, lastErr
}
