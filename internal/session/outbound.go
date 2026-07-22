package session

import (
	"sync"
	"time"
)

// Outbound is an optional post-login transport sink used by the gnet FSD plane.
// When set, Send / SendPosition route here instead of the channel + SenderWorker path.
//
// Implementations must be safe for concurrent Send / SendPosition from fan-out
// and handler goroutines. Close is idempotent.
//
// Contracts (same as Session.Send / SendPosition):
//   - Send may block under backpressure; returns error when closed / session done.
//   - SendPosition is non-blocking from the broadcaster’s perspective (may drop).
//   - TrySend (optional via type assert) is non-blocking reliable enqueue.
type Outbound interface {
	Send(packet string) error
	SendPosition(packet string) error
	Close() error
}

// errOutboundFull is returned by TrySend when the reliable ring is full.
var errOutboundFull = errFull{}

type errFull struct{}

func (errFull) Error() string { return "session: outbound full" }

// AsyncWriteFunc writes a complete buffer to the peer asynchronously.
// The buffer must not be retained by the caller after return; the implementation
// owns it (or copies). Must be concurrency-safe.
type AsyncWriteFunc func(p []byte) error

// CoalesceOutbound is a dual-queue, coalescing outbound sink for event-driven I/O.
//
// # Queues
//
//   - Reliable (Send): bounded ring; blocks when full (cap ReliableCap).
//   - Position (SendPosition): latest-wins single slot; never blocks; may drop.
//
// # Coalescing
//
// Small packets are appended into a write buffer and flushed via writeAsync when:
//   - buffer reaches CoalesceBytes, or
//   - a reliable/control packet is enqueued (flush immediately), or
//   - CoalesceIdle elapses after the first buffered position packet.
//
// There is no dedicated per-connection writer goroutine: flush runs under the
// mutex and hands bytes to writeAsync (typically gnet.Conn.AsyncWrite).
type CoalesceOutbound struct {
	writeAsync AsyncWriteFunc
	closeFn    func() error

	mu     sync.Mutex
	closed bool

	// Reliable ring buffer.
	rel    []string
	relR   int
	relW   int
	relN   int
	relCap int
	wait   *sync.Cond

	// Latest-wins position slot.
	pos    string
	hasPos bool

	// Coalesce assembly (packets already dequeued from rel/pos into buf).
	buf []byte

	// Idle flush for position-only traffic.
	idle     time.Duration
	coalesce int
	timer    *time.Timer
	timerOn  bool

	// Optional: invoked after a successful enqueue (before or after flush).
	onEnqueue func()
}

// CoalesceOutboundConfig tunes queue and flush behaviour.
type CoalesceOutboundConfig struct {
	// ReliableCap is the max number of reliable packets queued (default 32).
	ReliableCap int
	// CoalesceBytes flushes when the assemble buffer reaches this size (default 8KiB).
	CoalesceBytes int
	// CoalesceIdle is the max delay before flushing buffered position data (default 1ms).
	CoalesceIdle time.Duration
	// OnEnqueue is optional; invoked after a packet is accepted into a queue.
	OnEnqueue func()
}

// NewCoalesceOutbound builds an outbound sink. writeAsync must be non-nil.
// closeFn may be nil.
func NewCoalesceOutbound(writeAsync AsyncWriteFunc, closeFn func() error, cfg CoalesceOutboundConfig) *CoalesceOutbound {
	if writeAsync == nil {
		panic("session: NewCoalesceOutbound nil writeAsync")
	}
	if cfg.ReliableCap <= 0 {
		cfg.ReliableCap = sendChanCap
	}
	if cfg.CoalesceBytes <= 0 {
		cfg.CoalesceBytes = 8 * 1024
	}
	if cfg.CoalesceIdle <= 0 {
		cfg.CoalesceIdle = time.Millisecond
	}
	o := &CoalesceOutbound{
		writeAsync: writeAsync,
		closeFn:    closeFn,
		rel:        make([]string, cfg.ReliableCap),
		relCap:     cfg.ReliableCap,
		coalesce:   cfg.CoalesceBytes,
		idle:       cfg.CoalesceIdle,
		onEnqueue:  cfg.OnEnqueue,
	}
	o.wait = sync.NewCond(&o.mu)
	return o
}

// Send enqueues a reliable packet, blocking if the reliable ring is full.
func (o *CoalesceOutbound) Send(packet string) error {
	o.mu.Lock()
	for o.relN >= o.relCap && !o.closed {
		o.wait.Wait()
	}
	if o.closed {
		o.mu.Unlock()
		return errOutboundClosed
	}
	o.enqueueRelLocked(packet)
	// Control / reliable traffic flushes immediately so $ER / #TM are not delayed.
	o.drainToBufLocked()
	err := o.flushBufLocked()
	o.mu.Unlock()
	return err
}

// TrySend enqueues a reliable packet without blocking.
// Returns errOutboundFull if the ring is full, errOutboundClosed if closed.
func (o *CoalesceOutbound) TrySend(packet string) error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return errOutboundClosed
	}
	if o.relN >= o.relCap {
		o.mu.Unlock()
		return errOutboundFull
	}
	o.enqueueRelLocked(packet)
	o.drainToBufLocked()
	err := o.flushBufLocked()
	o.mu.Unlock()
	return err
}

func (o *CoalesceOutbound) enqueueRelLocked(packet string) {
	o.rel[o.relW] = packet
	o.relW++
	if o.relW >= o.relCap {
		o.relW = 0
	}
	o.relN++
	if o.onEnqueue != nil {
		o.onEnqueue()
	}
}

// SendPosition stores the latest position packet (droppable, non-blocking).
func (o *CoalesceOutbound) SendPosition(packet string) error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return errOutboundClosed
	}
	o.pos = packet
	o.hasPos = true
	if o.onEnqueue != nil {
		o.onEnqueue()
	}
	o.drainToBufLocked()
	if len(o.buf) >= o.coalesce {
		err := o.flushBufLocked()
		o.mu.Unlock()
		return err
	}
	o.armIdleLocked()
	o.mu.Unlock()
	return nil
}

// Close marks the outbound closed, wakes Send waiters, and closes the transport.
func (o *CoalesceOutbound) Close() error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
		o.timerOn = false
	}
	// Best-effort final flush.
	o.drainToBufLocked()
	_ = o.flushBufLocked()
	o.wait.Broadcast()
	o.mu.Unlock()
	if o.closeFn != nil {
		return o.closeFn()
	}
	return nil
}

func (o *CoalesceOutbound) drainToBufLocked() {
	for o.relN > 0 {
		p := o.rel[o.relR]
		o.rel[o.relR] = ""
		o.relR++
		if o.relR >= o.relCap {
			o.relR = 0
		}
		o.relN--
		o.buf = append(o.buf, p...)
	}
	o.wait.Broadcast() // free Send waiters
	if o.hasPos {
		o.buf = append(o.buf, o.pos...)
		o.pos = ""
		o.hasPos = false
	}
}

func (o *CoalesceOutbound) flushBufLocked() error {
	if len(o.buf) == 0 {
		return nil
	}
	// Hand ownership of buf to writeAsync; replace with a fresh slice.
	out := o.buf
	o.buf = make([]byte, 0, o.coalesce)
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
		o.timerOn = false
	}
	// Unlock during write so producers can enqueue; writeAsync is concurrency-safe.
	// We hold mu for simplicity and small flushes — AsyncWrite typically only queues.
	return o.writeAsync(out)
}

func (o *CoalesceOutbound) armIdleLocked() {
	if o.timerOn || o.closed {
		return
	}
	o.timerOn = true
	o.timer = time.AfterFunc(o.idle, o.idleFlush)
}

func (o *CoalesceOutbound) idleFlush() {
	o.mu.Lock()
	o.timerOn = false
	o.timer = nil
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.drainToBufLocked()
	_ = o.flushBufLocked()
	o.mu.Unlock()
}

// errOutboundClosed is returned when Send/SendPosition hit a closed outbound.
// Matches context cancellation behaviour at the Session layer via Ctx check first.
var errOutboundClosed = errClosed{}

type errClosed struct{}

func (errClosed) Error() string   { return "session: outbound closed" }
func (errClosed) Timeout() bool   { return false }
func (errClosed) Temporary() bool { return false }
