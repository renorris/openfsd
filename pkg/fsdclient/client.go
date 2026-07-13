package fsdclient

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/renorris/openfsd/pkg/protocol"
)

// Config configures Dial.
//
// Zero-value timeouts mean: DialTimeout defaults to 10s; ReadTimeout and
// WriteTimeout are not applied (no per-op deadline) unless set.
type Config struct {
	Addr         string
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	Clock        Clock // optional; nil => real wall clock
}

// Received is one inbound packet delivered by Next or WaitFor.
type Received struct {
	At   time.Time
	Raw  []byte
	Type protocol.PacketType
}

// Client is a protocol-faithful FSD TCP client.
//
// Concurrency model:
//   - Each Client is independent; N clients may run concurrently.
//   - Send and typed send helpers are safe for concurrent use (write mutex).
//   - Next is serialized with a read mutex; do not call Close concurrently
//     with an in-flight Next without expecting an error.
//   - Recorder is safe for concurrent inspection.
type Client struct {
	cfg   Config
	clock Clock
	rec   *Recorder

	conn net.Conn
	br   *bufio.Reader

	writeMu sync.Mutex
	readMu  sync.Mutex

	serverIdent protocol.ServerIdent

	// session state (guarded by writeMu for login mutation)
	loggedIn atomic.Bool
	isATC    atomic.Bool
	callsign atomic.Value // string
	closed   atomic.Bool
}

// ServerIdent returns the $DI payload received during Dial.
func (c *Client) ServerIdent() protocol.ServerIdent {
	return c.serverIdent
}

// Callsign returns the callsign set at login, or "" if not logged in.
func (c *Client) Callsign() string {
	v, _ := c.callsign.Load().(string)
	return v
}

// Recorder returns the packet recorder for this client.
func (c *Client) Recorder() *Recorder {
	return c.rec
}

// Send writes a raw packet to the connection. A trailing \r\n is appended
// when missing. Safe for concurrent use.
func (c *Client) Send(packet []byte) error {
	if c.closed.Load() {
		return ErrClosed
	}
	if c.conn == nil {
		return ErrNotDialed
	}
	wire := ensureCRLF(packet)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.setWriteDeadline(); err != nil {
		return err
	}
	_, err := c.conn.Write(wire)
	_ = c.clearWriteDeadline()
	if err != nil {
		return errf("fsdclient: send: %w", err)
	}
	c.rec.record(DirSent, wire)
	return nil
}

// Next reads the next complete line (\r\n-terminated) from the server.
// Only one Next may run at a time per Client.
func (c *Client) Next(ctx context.Context) (Received, error) {
	if c.closed.Load() {
		return Received{}, ErrClosed
	}
	if c.conn == nil {
		return Received{}, ErrNotDialed
	}
	if err := ctx.Err(); err != nil {
		return Received{}, err
	}

	c.readMu.Lock()
	defer c.readMu.Unlock()

	if c.closed.Load() {
		return Received{}, ErrClosed
	}

	// Arm cancellation: set a past read deadline when ctx is done.
	stop := context.AfterFunc(ctx, func() {
		_ = c.conn.SetReadDeadline(time.Now())
	})
	defer stop()

	if err := c.setReadDeadline(); err != nil {
		return Received{}, err
	}
	// Prefer ctx deadline when earlier than configured ReadTimeout.
	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetReadDeadline(dl)
	}

	line, err := c.br.ReadBytes('\n')
	_ = c.clearReadDeadline()
	if err != nil {
		if ctx.Err() != nil {
			return Received{}, ctx.Err()
		}
		if c.closed.Load() {
			return Received{}, ErrClosed
		}
		return Received{}, errf("fsdclient: read: %w", err)
	}

	// Normalize to include \r\n in stored form when possible.
	raw := bytes.TrimRight(line, "\r\n")
	wire := append(append([]byte{}, raw...), '\r', '\n')
	rec := c.rec.record(DirReceived, wire)
	return Received{At: rec.At, Raw: rec.Raw, Type: rec.Type}, nil
}

// WaitFor reads packets via Next until pred returns true or ctx ends.
// Unlike Recorder.WaitFor, this consumes from the network.
func (c *Client) WaitFor(ctx context.Context, pred func(Received) bool) (Received, error) {
	if pred == nil {
		return Received{}, errf("fsdclient: WaitFor nil predicate")
	}
	for {
		r, err := c.Next(ctx)
		if err != nil {
			return Received{}, err
		}
		if pred(r) {
			return r, nil
		}
	}
}

// Close closes the underlying connection. Optional ctx is reserved for
// future graceful teardown; it is currently only checked for already-canceled.
func (c *Client) Close(ctx context.Context) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			// Still attempt close.
			_ = err
		}
	}
	if !c.closed.CompareAndSwap(false, true) {
		return ErrClosed
	}
	c.rec.close()
	if c.conn != nil {
		err := c.conn.Close()
		if err != nil {
			return errf("fsdclient: close: %w", err)
		}
	}
	return nil
}

func (c *Client) setReadDeadline() error {
	if c.cfg.ReadTimeout <= 0 {
		return c.conn.SetReadDeadline(time.Time{})
	}
	return c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
}

func (c *Client) clearReadDeadline() error {
	return c.conn.SetReadDeadline(time.Time{})
}

func (c *Client) setWriteDeadline() error {
	if c.cfg.WriteTimeout <= 0 {
		return c.conn.SetWriteDeadline(time.Time{})
	}
	return c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
}

func (c *Client) clearWriteDeadline() error {
	return c.conn.SetWriteDeadline(time.Time{})
}

// ensureCRLF returns a copy of p ending with \r\n.
func ensureCRLF(p []byte) []byte {
	if bytes.HasSuffix(p, []byte("\r\n")) {
		return append([]byte(nil), p...)
	}
	if bytes.HasSuffix(p, []byte("\n")) {
		out := make([]byte, 0, len(p)+1)
		out = append(out, p[:len(p)-1]...)
		out = append(out, '\r', '\n')
		return out
	}
	out := make([]byte, 0, len(p)+2)
	out = append(out, p...)
	out = append(out, '\r', '\n')
	return out
}

// attachConn wires an already-connected net.Conn (used by Dial and tests).
func (c *Client) attachConn(conn net.Conn) {
	c.conn = conn
	c.br = bufio.NewReaderSize(conn, 4096)
}

// readServerIdent reads and validates the first $DI line.
func (c *Client) readServerIdent(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = c.conn.SetReadDeadline(time.Now())
	})
	defer stop()

	// Dial-phase read: use ReadTimeout if set, else DialTimeout, else 10s.
	timeout := c.cfg.ReadTimeout
	if timeout <= 0 {
		timeout = c.cfg.DialTimeout
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if dl, ok := ctx.Deadline(); ok {
		remain := time.Until(dl)
		if remain < timeout {
			timeout = remain
		}
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	line, err := c.br.ReadBytes('\n')
	_ = c.conn.SetReadDeadline(time.Time{})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errf("fsdclient: read server ident: %w", err)
	}

	raw := bytes.TrimRight(line, "\r\n")
	wire := append(append([]byte{}, raw...), '\r', '\n')

	// Require $DI and SERVER:CLIENT layout.
	if !bytes.HasPrefix(raw, []byte("$DISERVER:CLIENT:")) &&
		!(protocol.TypeOf(raw) == protocol.PacketTypeServerIdent &&
			bytes.HasPrefix(raw, []byte("$DI")) &&
			bytes.Contains(raw, []byte("SERVER:CLIENT"))) {
		// Still try parse, but also enforce SERVER:CLIENT fields.
		_ = c.rec.record(DirReceived, wire)
		return ErrBadServerIdent
	}

	// Field layout: $DISERVER : CLIENT : version : key
	// TypeOf/ParseServerIdent validate type + field count.
	if protocol.TypeOf(raw) != protocol.PacketTypeServerIdent {
		_ = c.rec.record(DirReceived, wire)
		return ErrBadServerIdent
	}
	// Explicit SERVER / CLIENT field check (fields 0 and 1 after prefix strip).
	// Field 0 is "$DISERVER", field 1 is "CLIENT".
	from := protocol.Field(raw, 0)
	to := protocol.Field(raw, 1)
	if !bytes.Equal(from, []byte("$DISERVER")) || !bytes.Equal(to, []byte("CLIENT")) {
		_ = c.rec.record(DirReceived, wire)
		return ErrBadServerIdent
	}

	ident, err := protocol.ParseServerIdent(raw)
	if err != nil {
		_ = c.rec.record(DirReceived, wire)
		return errf("%w: %v", ErrBadServerIdent, err)
	}
	c.serverIdent = ident
	c.rec.record(DirReceived, wire)
	return nil
}
