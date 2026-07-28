package afv

import (
	"context"
	"log/slog"
	"time"
)

func (s *Server) runReaper(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			n := s.reg.Reap(now)
			if n > 0 {
				slog.Info("AFV reaper removed sessions", "count", n)
			}
		}
	}
}
