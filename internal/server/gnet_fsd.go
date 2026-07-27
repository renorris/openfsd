package server

// Package-level FSD I/O plane notes (gnet path):
//
//   - Fixed number of gnet event-loop goroutines (NumEventLoop / GOMAXPROCS).
//   - One connection is sticky to one event loop; inbound line framing and
//     post-login dispatch run on that loop (no per-conn reader goroutine).
//   - Outbound uses session.CoalesceOutbound → gnet.Conn.AsyncWrite (no
//     per-conn SenderWorker). Reliable packets flush immediately; position
//     packets latest-wins and coalesce by size/idle.
//   - Login-phase sync writes use gnet.Conn.Write from OnTraffic (same loop).
//   - HTTP admin remains on net/http.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
	"golang.org/x/sys/unix"
)

const (
	fsdMaxLine   = 4096
	fsdPhaseOpen = iota
	fsdPhaseIdent
	fsdPhaseActive
)

// fsdConnCtx is per-connection state hung off gnet.Conn.Context.
type fsdConnCtx struct {
	phase    int
	lineBuf  []byte
	idPacket []byte

	client     *session.Session
	registered bool
	// disconnect broadcast deferred until OnClose after successful register
	// (mirrors gnet disconnect / synthetic cleanup broadcastDisconnectPacket).

	remoteIP   string
	connHeld   bool // limits.tryAcquireConn succeeded
	cidHeld    bool // limits.tryAcquireCID succeeded
	openedAt   time.Time
	lastActive time.Time
}

// fsdEngine is the gnet EventHandler for the FSD TCP plane.
type fsdEngine struct {
	gnet.BuiltinEventEngine

	srv    *Server
	ctx    context.Context
	cancel context.CancelFunc

	numLoops int
	addrs    []string // original listen addrs (host:port)

	eng   gnet.Engine
	engMu sync.Mutex

	// bound reports actual listen addresses after OnBoot (for :0).
	bound chan string

	// runErr is set when gnet.Rotate returns.
	runDone chan error
}

func newFSDEngine(srv *Server, parent context.Context, addrs []string, numLoops int, bound chan string) *fsdEngine {
	ctx, cancel := context.WithCancel(parent)
	if numLoops <= 0 {
		numLoops = runtime.GOMAXPROCS(0)
		if numLoops < 1 {
			numLoops = 1
		}
	}
	return &fsdEngine{
		srv:      srv,
		ctx:      ctx,
		cancel:   cancel,
		numLoops: numLoops,
		addrs:    addrs,
		bound:    bound,
		runDone:  make(chan error, 1),
	}
}

func (e *fsdEngine) OnBoot(eng gnet.Engine) (action gnet.Action) {
	e.engMu.Lock()
	e.eng = eng
	e.engMu.Unlock()

	// Report bound addresses (handles :0 ephemeral ports).
	if e.bound != nil {
		for _, a := range e.addrs {
			network, hostport := splitProtoAddr(a)
			if hostport == "" {
				hostport = a
			}
			if addr, err := listenerAddr(eng, network, hostport); err == nil && addr != "" {
				select {
				case e.bound <- addr:
				default:
				}
			}
		}
	}
	return gnet.None
}

func (e *fsdEngine) OnShutdown(_ gnet.Engine) {
	e.cancel()
}

func (e *fsdEngine) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	now := e.srv.clock.Now()
	ip := ""
	if ra := c.RemoteAddr(); ra != nil {
		host, _, err := net.SplitHostPort(ra.String())
		if err != nil {
			host = ra.String()
		}
		ip = host
	}

	if !e.srv.limits.tryAcquireConn(ip, e.srv.cfg.FsdMaxConnections, e.srv.cfg.FsdMaxConnectionsPerIP) {
		e.srv.logger.Debug("gnet connection rejected: connection limit", "ip", ip)
		return nil, gnet.Close
	}

	cc := &fsdConnCtx{
		phase:      fsdPhaseOpen,
		lineBuf:    make([]byte, 0, 256),
		remoteIP:   ip,
		connHeld:   true,
		openedAt:   now,
		lastActive: now,
	}
	c.SetContext(cc)

	// Server ident (same bytes as sendServerIdent).
	packet := protocol.ServerIdent{
		Version:      "openfsd",
		ChallengeKey: "6f70656e667364",
	}.Marshal()
	cc.phase = fsdPhaseIdent
	return packet, gnet.None
}

func (e *fsdEngine) OnClose(c gnet.Conn, _ error) (action gnet.Action) {
	cc, _ := c.Context().(*fsdConnCtx)
	if cc == nil {
		return gnet.None
	}
	if cc.client != nil {
		if o := cc.client.Outbound(); o != nil {
			_ = o.Close()
		}
		if cc.registered {
			e.srv.broadcastDisconnectPacket(cc.client)
			e.srv.registry.Release(cc.client)
			cc.registered = false
		}
		if cc.cidHeld {
			e.srv.limits.releaseCID(cc.client.CID)
			cc.cidHeld = false
		}
		cc.client.Cancel()
	}
	if cc.connHeld {
		e.srv.limits.releaseConn(cc.remoteIP)
		cc.connHeld = false
	}
	return gnet.None
}

func (e *fsdEngine) OnTraffic(c gnet.Conn) (action gnet.Action) {
	cc, _ := c.Context().(*fsdConnCtx)
	if cc == nil {
		return gnet.Close
	}

	now := e.srv.clock.Now()
	// Login-phase timeout.
	if cc.phase == fsdPhaseIdent && e.srv.cfg.FsdLoginTimeout > 0 {
		if now.Sub(cc.openedAt) > e.srv.cfg.FsdLoginTimeout {
			return gnet.Close
		}
	}
	// Post-login idle timeout.
	if cc.phase == fsdPhaseActive && e.srv.cfg.FsdIdleTimeout > 0 {
		if now.Sub(cc.lastActive) > e.srv.cfg.FsdIdleTimeout {
			return gnet.Close
		}
	}

	// Append inbound bytes into line buffer (must copy — gnet reuses buffers).
	for {
		buf, err := c.Next(-1)
		if len(buf) > 0 {
			// Cap growth while framing to avoid multi-line amplification.
			if len(cc.lineBuf)+len(buf) > fsdMaxLine*4 {
				return gnet.Close
			}
			cc.lineBuf = append(cc.lineBuf, buf...)
		}
		if err != nil || len(buf) == 0 {
			break
		}
	}

	for {
		line, ok := popLine(&cc.lineBuf)
		if !ok {
			break
		}
		if len(line) > fsdMaxLine {
			return gnet.Close
		}
		if len(cc.lineBuf) > fsdMaxLine*4 {
			// Runaway buffer without delimiters.
			return gnet.Close
		}
		if action = e.handleLine(c, cc, line); action != gnet.None {
			return action
		}
	}
	if len(cc.lineBuf) > fsdMaxLine {
		return gnet.Close
	}
	return gnet.None
}

func (e *fsdEngine) handleLine(c gnet.Conn, cc *fsdConnCtx, line []byte) gnet.Action {
	cc.lastActive = e.srv.clock.Now()
	switch cc.phase {
	case fsdPhaseIdent:
		if cc.idPacket == nil {
			// First login line: client ident (copy — line may alias lineBuf).
			cc.idPacket = append([]byte(nil), line...)
			return gnet.None
		}
		// Second login line: add packet.
		addPacket := line
		return e.finishLogin(c, cc, cc.idPacket, addPacket)

	case fsdPhaseActive:
		return e.dispatchActive(cc, line)

	default:
		return gnet.Close
	}
}

func (e *fsdEngine) finishLogin(c gnet.Conn, cc *fsdConnCtx, idPacket, addPacket []byte) gnet.Action {
	data, token, errCode, errMsg, err := parseLoginPackets(idPacket, addPacket, e.srv.clock.Now())
	if err != nil {
		_ = writeLoginError(c, errCode, errMsg)
		return gnet.Close
	}
	if !isValidClientCallsign([]byte(data.Callsign)) {
		_ = writeLoginError(c, CallsignInvalidError, "Callsign invalid")
		return gnet.Close
	}

	client := session.New(e.ctx, nil, nil, data)
	client.Auth = &auth.AuthState{}
	if cc.remoteIP != "" {
		client.SetRemoteIP(cc.remoteIP)
	} else if ra := c.RemoteAddr(); ra != nil {
		host, _, splitErr := net.SplitHostPort(ra.String())
		if splitErr != nil {
			host = ra.String()
		}
		client.SetRemoteIP(host)
		cc.remoteIP = host
	}
	client.LastInboundNs.Store(e.srv.clock.Now().UnixNano())

	// Wire coalescing AsyncWrite outbound (no SenderWorker).
	gc := c
	out, outErr := session.NewCoalesceOutbound(
		func(p []byte) error {
			return gc.AsyncWrite(p, nil)
		},
		func() error {
			return gc.Close()
		},
		session.CoalesceOutboundConfig{},
	)
	if outErr != nil {
		// Defensive: writeAsync is non-nil above; should never fire in production.
		e.srv.logger.Error("coalesce outbound", "err", outErr)
		return gnet.Close
	}
	client.SetOutbound(out)
	cc.client = client

	// attemptAuthentication uses client.Conn for login-phase errors; Conn is nil
	// on gnet path — use a one-shot writer adapter via writeLoginError on failure.
	if err = e.attemptAuthGnet(c, client, token); err != nil {
		_ = out.Close()
		return gnet.Close
	}

	if !e.srv.limits.tryAcquireCID(client.CID, e.srv.cfg.FsdMaxSessionsPerCID) {
		_ = writeLoginError(c, ServerFullError, "Too many sessions for this CID")
		_ = out.Close()
		return gnet.Close
	}
	cc.cidHeld = true

	if err = e.srv.registry.Register(client); err != nil {
		if errors.Is(err, ErrCallsignInUse) {
			_ = writeLoginError(c, CallsignInUseError, "Callsign already in use")
		}
		e.srv.limits.releaseCID(client.CID)
		cc.cidHeld = false
		_ = out.Close()
		return gnet.Close
	}
	cc.registered = true

	if err = e.srv.sendMotd(client); err != nil {
		client.Disconnect()
		return gnet.Close
	}

	e.srv.broadcastAddPacket(client)
	cc.phase = fsdPhaseActive
	cc.idPacket = nil
	cc.lastActive = e.srv.clock.Now()
	return gnet.None
}

// attemptAuthGnet reuses Server.attemptAuthentication with a temporary
// net.Conn adapter so login-phase sendError(client.Conn, ...) writes via gnet.
func (e *fsdEngine) attemptAuthGnet(c gnet.Conn, client *session.Session, token string) error {
	adapter := &gnetLoginConn{gc: c, remote: c.RemoteAddr(), local: c.LocalAddr()}
	client.Conn = adapter
	err := e.srv.attemptAuthentication(client, token)
	// Clear temporary login adapter so post-login path cannot Conn.Write.
	client.Conn = nil
	return err
}

func (e *fsdEngine) dispatchActive(cc *fsdConnCtx, line []byte) gnet.Action {
	client := cc.client
	if client == nil || client.Ctx.Err() != nil {
		return gnet.Close
	}

	// Immutable packet with CRLF for handlers / fan-out.
	packet := make([]byte, len(line)+2)
	copy(packet, line)
	packet[len(line)] = '\r'
	packet[len(line)+1] = '\n'

	client.LastInboundNs.Store(e.srv.clock.Now().UnixNano())

	packetType, ok := verifyPacket(packet, client)
	if !ok {
		return gnet.None
	}
	handler := e.srv.getHandler(packetType)
	handler(client, packet)

	if client.Ctx.Err() != nil {
		return gnet.Close
	}
	return gnet.None
}

// run starts gnet.Rotate and blocks until the engine stops.
func (e *fsdEngine) run() error {
	protoAddrs := make([]string, len(e.addrs))
	for i, a := range e.addrs {
		protoAddrs[i] = toGnetAddr(a)
	}

	opts := []gnet.Option{
		gnet.WithMulticore(true),
		gnet.WithNumEventLoop(e.numLoops),
		gnet.WithReuseAddr(true),
		gnet.WithTCPNoDelay(gnet.TCPNoDelay),
		gnet.WithTCPKeepAlive(60 * time.Second),
		gnet.WithReadBufferCap(fsdMaxLine * 2),
		gnet.WithWriteBufferCap(64 * 1024),
	}

	// Stop engine when parent context cancels.
	go func() {
		<-e.ctx.Done()
		e.engMu.Lock()
		eng := e.eng
		e.engMu.Unlock()
		if eng.Validate() == nil {
			_ = eng.Stop(context.Background())
		}
	}()

	var err error
	if len(protoAddrs) == 1 {
		err = gnet.Run(e, protoAddrs[0], opts...)
	} else {
		err = gnet.Rotate(e, protoAddrs, opts...)
	}
	return err
}

// --- helpers ---

func toGnetAddr(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	// Default FSD to IPv4 TCP (historical tcp4 bind preference).
	if strings.HasPrefix(addr, ":") {
		return "tcp4://0.0.0.0" + addr
	}
	return "tcp4://" + addr
}

func splitProtoAddr(addr string) (network, hostport string) {
	if i := strings.Index(addr, "://"); i >= 0 {
		return addr[:i], addr[i+3:]
	}
	return "tcp4", addr
}

func listenerAddr(eng gnet.Engine, network, hostport string) (string, error) {
	fd, err := eng.DupListener(network, hostport)
	if err != nil {
		// Single-listener engines may only support Dup().
		fd, err = eng.Dup()
		if err != nil {
			return "", err
		}
	}
	defer unix.Close(fd)

	sa, err := unix.Getsockname(fd)
	if err != nil {
		return "", err
	}
	switch v := sa.(type) {
	case *unix.SockaddrInet4:
		ip := net.IPv4(v.Addr[0], v.Addr[1], v.Addr[2], v.Addr[3])
		return net.JoinHostPort(ip.String(), fmt.Sprint(v.Port)), nil
	case *unix.SockaddrInet6:
		ip := net.IP(v.Addr[:]).To16()
		return net.JoinHostPort(ip.String(), fmt.Sprint(v.Port)), nil
	default:
		return "", fmt.Errorf("unsupported sockaddr %T", sa)
	}
}

// popLine extracts one CRLF/LF-delimited line from buf (without delimiter).
func popLine(buf *[]byte) (line []byte, ok bool) {
	b := *buf
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		return nil, false
	}
	line = b[:i]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	// Copy line; advance buffer.
	out := append([]byte(nil), line...)
	*buf = append([]byte{}, b[i+1:]...)
	return out, true
}

func writeLoginError(c gnet.Conn, code int, message string) error {
	pkt := protocol.FormatError(protocol.ErrorCode(code), message)
	_, err := c.Write([]byte(pkt))
	return err
}

// gnetLoginConn is a minimal net.Conn used only during login-phase sendError.
type gnetLoginConn struct {
	gc     gnet.Conn
	remote net.Addr
	local  net.Addr
}

func (g *gnetLoginConn) Read(b []byte) (int, error)         { return 0, net.ErrClosed }
func (g *gnetLoginConn) Write(b []byte) (int, error)        { return g.gc.Write(b) }
func (g *gnetLoginConn) Close() error                       { return g.gc.Close() }
func (g *gnetLoginConn) LocalAddr() net.Addr                { return g.local }
func (g *gnetLoginConn) RemoteAddr() net.Addr               { return g.remote }
func (g *gnetLoginConn) SetDeadline(t time.Time) error      { return nil }
func (g *gnetLoginConn) SetReadDeadline(t time.Time) error  { return nil }
func (g *gnetLoginConn) SetWriteDeadline(t time.Time) error { return nil }
