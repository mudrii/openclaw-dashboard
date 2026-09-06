package appserver

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mudrii/openclaw-dashboard/internal/appopenclaw"
	"github.com/mudrii/openclaw-dashboard/internal/apprefresh"
)

const (
	// runtimeLogCacheTTL bounds how long one gateway log read is reused.
	runtimeLogCacheTTL = 5 * time.Second
	// runtimeLogReadTimeout bounds one detached gateway log read. It sits above
	// the client's own per-call bound so the client classifies its own timeout.
	runtimeLogReadTimeout = 15 * time.Second
)

// runtimeLogRead is one gateway log read shared by every caller that arrives
// while it runs. Its fields are written before done is closed and read only
// after, so the close provides the happens-before edge.
type runtimeLogRead struct {
	done     chan struct{}
	snapshot apprefresh.RuntimeLogs
	err      error
}

// runtimeLogCache holds the newest gateway log read shared by all pollers.
// inflight is non-nil while a read is running: concurrent callers wait on it
// instead of launching a second CLI process, and the mutex is never held
// across the exec.
type runtimeLogCache struct {
	mu       sync.Mutex
	at       time.Time
	snapshot apprefresh.RuntimeLogs
	err      error
	inflight *runtimeLogRead
}

// Fast browser polling must not launch a CLI process for every request.
func (s *Server) readRuntimeLogs(ctx context.Context, limit int) (apprefresh.RuntimeLogs, error) {
	result, err := s.runtimeLogs.read(ctx, func(readCtx context.Context) (apprefresh.RuntimeLogs, error) {
		return apprefresh.ReadRuntimeLogs(appopenclaw.WithTarget(readCtx, s.cfg.Openclaw), s.runtimeClient, logLimitMax)
	})
	if err != nil {
		return result, err
	}
	if len(result.Entries) > limit {
		result.Entries = result.Entries[len(result.Entries)-limit:]
		result.Truncated = true
	}
	return result, nil
}

// read returns the cached snapshot, or runs fetch once on behalf of every
// waiting caller. A caller that has already gone away neither starts nor
// caches work, so one abandoned poll cannot make the next 5 s of polls fail.
func (c *runtimeLogCache) read(ctx context.Context, fetch func(context.Context) (apprefresh.RuntimeLogs, error)) (apprefresh.RuntimeLogs, error) {
	if err := ctx.Err(); err != nil {
		return apprefresh.RuntimeLogs{}, err
	}

	c.mu.Lock()
	if time.Since(c.at) < runtimeLogCacheTTL {
		snapshot, err := c.snapshot, c.err
		c.mu.Unlock()
		return snapshot, err
	}
	if inflight := c.inflight; inflight != nil {
		c.mu.Unlock()
		select {
		case <-inflight.done:
			return inflight.snapshot, inflight.err
		case <-ctx.Done():
			return apprefresh.RuntimeLogs{}, ctx.Err()
		}
	}
	inflight := &runtimeLogRead{done: make(chan struct{})}
	c.inflight = inflight
	c.mu.Unlock()
	defer close(inflight.done)

	// Detached: one client disconnecting must not kill a read the remaining
	// clients are waiting on. The bound replaces the caller's cancellation.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeLogReadTimeout)
	defer cancel()
	inflight.snapshot, inflight.err = fetch(readCtx)

	c.mu.Lock()
	c.inflight = nil
	if !callerAborted(ctx, inflight.err) {
		c.snapshot, c.err, c.at = inflight.snapshot, inflight.err, time.Now()
	}
	c.mu.Unlock()
	return inflight.snapshot, inflight.err
}

// callerAborted reports whether err describes the caller giving up rather than
// a fault in the runtime read itself. Such a result says nothing about the
// runtime's health, so it must never be cached for other callers.
func callerAborted(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
