package fsdclient

import (
	"context"
	"sync"
	"time"

	"github.com/renorris/openfsd/pkg/protocol"
)

// Direction indicates whether a packet was sent or received.
type Direction int

const (
	// DirSent marks an outbound packet.
	DirSent Direction = iota
	// DirReceived marks an inbound packet.
	DirReceived
)

// Record is one recorded packet with a timestamp.
type Record struct {
	At   time.Time
	Dir  Direction
	Raw  []byte
	Type protocol.PacketType
}

// Recorder stores all sent and received packets with timestamps.
// It is safe for concurrent use.
//
// Concurrency model for the parent Client:
//   - Send (and typed send helpers) may be called concurrently; writes are serialized.
//   - Next must not be called concurrently with itself (a read mutex enforces this).
//   - Multiple independent Client instances may be used concurrently (N clients).
//
// WaitFor watches packets already recorded as received. It does not read the
// network; pair it with a pump that calls Client.Next, or use Client.WaitFor
// which reads until the predicate matches.
type Recorder struct {
	mu      sync.Mutex
	cond    *sync.Cond
	clock   Clock
	records []Record
	closed  bool
}

func newRecorder(clock Clock) *Recorder {
	r := &Recorder{clock: resolveClock(clock)}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// record appends a packet in the given direction.
func (r *Recorder) record(dir Direction, raw []byte) Record {
	rawCopy := append([]byte(nil), raw...)
	rec := Record{
		At:   r.clock.Now(),
		Dir:  dir,
		Raw:  rawCopy,
		Type: protocol.TypeOf(rawCopy),
	}
	r.mu.Lock()
	r.records = append(r.records, rec)
	if dir == DirReceived {
		r.cond.Broadcast()
	}
	r.mu.Unlock()
	return rec
}

// All returns a snapshot of all recorded packets (copy of the slice; raw
// bytes are already owned copies).
func (r *Recorder) All() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, len(r.records))
	copy(out, r.records)
	return out
}

// Sent returns a snapshot of outbound records only.
func (r *Recorder) Sent() []Record {
	return r.filter(DirSent)
}

// Received returns a snapshot of inbound records only.
func (r *Recorder) Received() []Record {
	return r.filter(DirReceived)
}

func (r *Recorder) filter(dir Direction) []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Record
	for _, rec := range r.records {
		if rec.Dir == dir {
			out = append(out, rec)
		}
	}
	return out
}

// Len returns the number of recorded packets.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// close wakes all WaitFor waiters.
func (r *Recorder) close() {
	r.mu.Lock()
	r.closed = true
	r.cond.Broadcast()
	r.mu.Unlock()
}

// WaitFor blocks until a received packet matching pred has been recorded, or
// until ctx is done / the recorder is closed.
//
// Already-recorded received packets are considered first (from index 0).
// Predicates should be pure; bare sleep loops without a predicate are not used.
func (r *Recorder) WaitFor(ctx context.Context, pred func(Received) bool) (Received, error) {
	if pred == nil {
		return Received{}, errf("fsdclient: WaitFor nil predicate")
	}

	// Wake on context cancellation.
	stop := context.AfterFunc(ctx, func() {
		r.mu.Lock()
		r.cond.Broadcast()
		r.mu.Unlock()
	})
	defer stop()

	r.mu.Lock()
	defer r.mu.Unlock()

	start := 0
	for {
		for i := start; i < len(r.records); i++ {
			rec := r.records[i]
			if rec.Dir != DirReceived {
				continue
			}
			got := Received{At: rec.At, Raw: rec.Raw, Type: rec.Type}
			if pred(got) {
				return got, nil
			}
		}
		start = len(r.records)

		if err := ctx.Err(); err != nil {
			return Received{}, err
		}
		if r.closed {
			return Received{}, errf("fsdclient: recorder closed")
		}
		r.cond.Wait()
	}
}
