package afv

import (
	"testing"
	"time"
)

func TestDropOldestQueue_VoiceDepth(t *testing.T) {
	q := newDropOldestQueue[int](meshVoiceQueueDepth)
	for i := 0; i < meshVoiceQueueDepth; i++ {
		if q.Enqueue(i) {
			t.Fatalf("unexpected drop at %d", i)
		}
	}
	if q.Len() != meshVoiceQueueDepth {
		t.Fatalf("len=%d", q.Len())
	}
	// push more → drop oldest
	for i := 0; i < 10; i++ {
		if !q.Enqueue(1000 + i) {
			t.Fatalf("expected drop on overflow %d", i)
		}
	}
	if q.Drops() != 10 {
		t.Fatalf("drops=%d", q.Drops())
	}
	// oldest should be 10 (0..9 dropped)
	v, ok := q.TryRecv()
	if !ok || v != 10 {
		t.Fatalf("got %v ok=%v want 10", v, ok)
	}
}

func TestDropOldestQueue_ControlDepth(t *testing.T) {
	q := newDropOldestQueue[meshCtrlJob](meshControlQueueDepth)
	for i := 0; i < meshControlQueueDepth; i++ {
		q.Enqueue(meshCtrlJob{typ: MeshTypeHeartbeat, payload: []byte{byte(i)}})
	}
	drops := 0
	for i := 0; i < 5; i++ {
		if q.Enqueue(meshCtrlJob{typ: MeshTypeHeartbeat, payload: []byte{0xff}}) {
			drops++
		}
	}
	if drops != 5 || q.Drops() != 5 {
		t.Fatalf("drops=%d q.Drops=%d", drops, q.Drops())
	}
	if q.Cap() != meshControlQueueDepth {
		t.Fatal()
	}
}

func TestDropOldestQueue_UnbufferedRacePath(t *testing.T) {
	// Unbuffered channel forces drop-retry path without concurrent producers.
	q := &dropOldestQueue[int]{ch: make(chan int)}
	if !q.Enqueue(1) {
		// may drop after retries
	}
	if q.Drops() == 0 {
		t.Fatal("expected drops on unbuffered enqueue")
	}
}

func TestDropOldestQueue_Helpers(t *testing.T) {
	q := newDropOldestQueue[int](0) // min cap 1
	if q.Cap() != 1 {
		t.Fatal(q.Cap())
	}
	q.Enqueue(1)
	// Recv blocking path via goroutine
	done := make(chan int, 1)
	go func() {
		v, ok := q.Recv()
		if ok {
			done <- v
		}
	}()
	select {
	case v := <-done:
		if v != 1 {
			t.Fatal(v)
		}
	case <-time.After(time.Second):
		t.Fatal("Recv timeout")
	}
	// Chan select
	q2 := newDropOldestQueue[int](2)
	q2.Enqueue(9)
	select {
	case v := <-q2.Chan():
		if v != 9 {
			t.Fatal(v)
		}
	default:
		t.Fatal("empty chan")
	}
	// TryRecv empty
	if _, ok := q2.TryRecv(); ok {
		t.Fatal()
	}
	// Enqueue when full with concurrent drain race path — fill and drop
	q3 := newDropOldestQueue[int](2)
	q3.Enqueue(1)
	q3.Enqueue(2)
	if !q3.Enqueue(3) {
		t.Fatal("expected drop")
	}
}

func TestMemoryMesh_QueueDropCounters(t *testing.T) {
	hub := NewMemoryHub()
	m1, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = m2
	// fill voice beyond capacity
	m1.ForceEnqueueVoiceForTest("n2", meshVoiceQueueDepth+20, AudioRelay{
		Callsign: "X", Audio: []byte{1},
	})
	if m1.VoiceDrops() < 20 {
		t.Fatalf("voice drops=%d", m1.VoiceDrops())
	}
	m1.ForceEnqueueCtrlForTest("n2", meshControlQueueDepth+10)
	if m1.CtrlDrops() < 10 {
		t.Fatalf("ctrl drops=%d", m1.CtrlDrops())
	}
}
