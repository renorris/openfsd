package afv

import "sync/atomic"

// dropOldestQueue is a fixed-depth ring used for mesh outbound jobs.
// Enqueue is non-blocking: if full, drops the oldest element then pushes.
type dropOldestQueue[T any] struct {
	ch    chan T
	drops atomic.Uint64
}

func newDropOldestQueue[T any](cap int) *dropOldestQueue[T] {
	if cap < 1 {
		cap = 1
	}
	return &dropOldestQueue[T]{ch: make(chan T, cap)}
}

// Enqueue pushes v. If full, drains one oldest then pushes (drop-oldest).
// Never blocks the caller on a full queue (may briefly block if concurrent
// drain races; production writers are single-threaded per peer).
func (q *dropOldestQueue[T]) Enqueue(v T) (dropped bool) {
	select {
	case q.ch <- v:
		return false
	default:
	}
	// Full: drop oldest, then push. Retry a few times for concurrent drain races.
	for attempt := 0; attempt < 4; attempt++ {
		select {
		case <-q.ch:
			q.drops.Add(1)
			dropped = true
		default:
		}
		select {
		case q.ch <- v:
			return dropped
		default:
			// concurrent consumer emptied slot then another producer filled — retry
		}
	}
	q.drops.Add(1)
	return true
}

// TryRecv non-blocking receive.
func (q *dropOldestQueue[T]) TryRecv() (v T, ok bool) {
	select {
	case v = <-q.ch:
		return v, true
	default:
		var zero T
		return zero, false
	}
}

// Recv blocking receive (or until closed).
func (q *dropOldestQueue[T]) Recv() (v T, ok bool) {
	v, ok = <-q.ch
	return v, ok
}

// Chan exposes the underlying channel for select loops.
func (q *dropOldestQueue[T]) Chan() <-chan T {
	return q.ch
}

// Drops returns how many oldest elements were discarded.
func (q *dropOldestQueue[T]) Drops() uint64 {
	return q.drops.Load()
}

// Len returns approximate buffered count.
func (q *dropOldestQueue[T]) Len() int {
	return len(q.ch)
}

// Cap returns capacity.
func (q *dropOldestQueue[T]) Cap() int {
	return cap(q.ch)
}

// meshVoiceQueueDepth is AudioRelay outbound depth (M-6).
const meshVoiceQueueDepth = 256

// meshControlQueueDepth is Snapshot/Delta/Leave/Interest/HB depth (M-6).
const meshControlQueueDepth = 64
