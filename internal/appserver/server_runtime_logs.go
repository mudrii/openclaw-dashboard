package appserver

import (
	"context"
	"sync"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

type runtimeLogCache struct {
	mu       sync.Mutex
	at       time.Time
	snapshot apprefresh.RuntimeLogs
	err      error
}

// Fast browser polling must not launch a CLI process for every request.
func (s *Server) readRuntimeLogs(ctx context.Context, limit int) (apprefresh.RuntimeLogs, error) {
	s.runtimeLogs.mu.Lock()
	defer s.runtimeLogs.mu.Unlock()
	if time.Since(s.runtimeLogs.at) >= 5*time.Second {
		ctx = appopenclaw.WithTarget(ctx, s.cfg.Openclaw)
		s.runtimeLogs.snapshot, s.runtimeLogs.err = apprefresh.ReadRuntimeLogs(ctx, s.runtimeClient, logLimitMax)
		s.runtimeLogs.at = time.Now()
	}
	result := s.runtimeLogs.snapshot
	if len(result.Entries) > limit {
		result.Entries = result.Entries[len(result.Entries)-limit:]
		result.Truncated = true
	}
	return result, s.runtimeLogs.err
}
