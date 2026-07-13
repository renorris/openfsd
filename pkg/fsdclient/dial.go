package fsdclient

import (
	"context"
	"net"
	"time"
)

// Dial connects to cfg.Addr, reads the server $DI identification packet,
// and returns a ready Client. The first line must be $DISERVER:CLIENT:...
// Version and challenge key are stored on Client.ServerIdent().
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Addr == "" {
		return nil, errf("fsdclient: empty Addr")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}

	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, errf("fsdclient: dial %s: %w", cfg.Addr, err)
	}

	c, err := newClientFromConn(ctx, cfg, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// DialConn is like Dial but uses an existing connection (e.g. net.Pipe or a
// test stub). It still expects and consumes the server $DI line.
func DialConn(ctx context.Context, cfg Config, conn net.Conn) (*Client, error) {
	if conn == nil {
		return nil, errf("fsdclient: nil conn")
	}
	return newClientFromConn(ctx, cfg, conn)
}

func newClientFromConn(ctx context.Context, cfg Config, conn net.Conn) (*Client, error) {
	clock := resolveClock(cfg.Clock)
	c := &Client{
		cfg:   cfg,
		clock: clock,
		rec:   newRecorder(clock),
	}
	c.callsign.Store("")
	c.attachConn(conn)

	if err := c.readServerIdent(ctx); err != nil {
		return nil, err
	}
	return c, nil
}
