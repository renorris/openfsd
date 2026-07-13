package fsdclient

import (
	"bufio"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// startStubServer listens on a random port, sends $DI, then runs handler.
func startStubServer(t *testing.T, handler func(net.Conn)) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				// Always send openfsd $DI first.
				_, _ = c.Write([]byte("$DISERVER:CLIENT:openfsd:6f70656e667364\r\n"))
				if handler != nil {
					handler(c)
				} else {
					// Drain until client closes.
					_, _ = io.Copy(io.Discard, c)
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		wg.Wait()
	}
}

// lineCollector captures inbound client lines without sleep-polling.
// After want lines are seen (or the connection ends), Wait unblocks.
type lineCollector struct {
	mu   sync.Mutex
	got  []string
	want int
	done chan struct{}
	once sync.Once
}

func newLineCollector(want int) *lineCollector {
	return &lineCollector{want: want, done: make(chan struct{})}
}

func (lc *lineCollector) handler(conn net.Conn) {
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		lc.mu.Lock()
		lc.got = append(lc.got, sc.Text())
		n := len(lc.got)
		lc.mu.Unlock()
		if lc.want > 0 && n >= lc.want {
			lc.once.Do(func() { close(lc.done) })
		}
	}
	lc.once.Do(func() { close(lc.done) })
}

// Wait blocks until want lines (or EOF) or ctx ends. Returns a copy of lines.
func (lc *lineCollector) Wait(ctx context.Context) []string {
	select {
	case <-lc.done:
	case <-ctx.Done():
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return append([]string(nil), lc.got...)
}

// waitClosed closes client and waits for the collector (EOF path).
func waitClosed(t *testing.T, c *Client, lc *lineCollector) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.Close(ctx)
	return lc.Wait(ctx)
}
