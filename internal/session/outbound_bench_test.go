package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// Compare classic channel+SenderWorker enqueue vs CoalesceOutbound under
// fan-out shaped load (one producer → many recipients).

func BenchmarkFanoutEnqueue_Serial(b *testing.B) {
	const recipients = 800
	pkt := "@S:N0001:1200:1:33.94000:-118.40000:5000:250:0:0\r\n"

	b.Run("channel", func(b *testing.B) {
		peers := make([]*Session, recipients)
		cancels := make([]context.CancelFunc, recipients)
		for i := 0; i < recipients; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			cancels[i] = cancel
			s := New(ctx, nil, nil, LoginData{Callsign: "P"})
			// Drain immediately so SendPosition never hits full-buffer path noise.
			s.SetSendEnqueueObserver(func(string, time.Time, int) {
				_, _ = s.DequeueOutbound()
			})
			go s.SenderWorker()
			peers[i] = s
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, p := range peers {
				_ = p.SendPosition(pkt)
			}
		}
		b.StopTimer()
		for _, c := range cancels {
			c()
		}
	})

	b.Run("coalesce", func(b *testing.B) {
		peers := make([]*Session, recipients)
		outs := make([]*CoalesceOutbound, recipients)
		var writes atomic.Int64
		for i := 0; i < recipients; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			_ = cancel
			o, err := NewCoalesceOutbound(func(p []byte) error {
				writes.Add(1)
				return nil
			}, nil, CoalesceOutboundConfig{
				CoalesceBytes: 8 * 1024,
				CoalesceIdle:  time.Millisecond,
			})
			if err != nil {
				b.Fatal(err)
			}
			s := New(ctx, nil, nil, LoginData{Callsign: "P"})
			s.SetOutbound(o)
			peers[i] = s
			outs[i] = o
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, p := range peers {
				_ = p.SendPosition(pkt)
			}
		}
		b.StopTimer()
		for _, o := range outs {
			_ = o.Close()
		}
		b.ReportMetric(float64(writes.Load())/float64(b.N), "async_writes/op")
	})
}

func BenchmarkSendPosition_ParallelOnePeer(b *testing.B) {
	pkt := "@S:N0001:1200:1:33.94000:-118.40000:5000:250:0:0\r\n"

	b.Run("channel", func(b *testing.B) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := New(ctx, nil, nil, LoginData{Callsign: "PEER"})
		s.SetSendEnqueueObserver(func(string, time.Time, int) {
			_, _ = s.DequeueOutbound()
		})
		go s.SenderWorker()
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = s.SendPosition(pkt)
			}
		})
	})

	b.Run("coalesce", func(b *testing.B) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		o, err := NewCoalesceOutbound(func(p []byte) error { return nil }, nil, CoalesceOutboundConfig{
			CoalesceBytes: 8 * 1024,
			CoalesceIdle:  time.Millisecond,
		})
		if err != nil {
			b.Fatal(err)
		}
		s := New(ctx, nil, nil, LoginData{Callsign: "PEER"})
		s.SetOutbound(o)
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_ = s.SendPosition(pkt)
			}
		})
		_ = o.Close()
	})
}
