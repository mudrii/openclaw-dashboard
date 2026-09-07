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
	result, err := s.runtimeLogs.read(ctx, func() (apprefresh.RuntimeLogs, error) {
		// Detached from the caller: one client disconnecting must not kill a
		// read the remaining clients are waiting on. The server's lifecycle
		// context still cancels the child process on shutdown.
		readCtx, cancel := context.WithTimeout(appopenclaw.WithTarget(s.ctx, s.cfg.Openclaw), runtimeLogReadTimeout)
		defer cancel()
		return apprefresh.ReadRuntimeLogs(readCtx, s.runtimeClient, logLimitMax)
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

// errRuntimeLogReadAborted reports that the shared read never produced a
// result, so waiters must not read the zero snapshot as an empty log page.
var errRuntimeLogReadAborted = errors.New("runtime log read aborted")

// read returns the cached snapshot, or runs fetch once on behalf of every
// waiting caller. A caller that has already gone away does not start work, so
// one abandoned poll cannot make the next 5 s of polls fail. fetch owns its own
// context: its result is shared, so it must outlive whichever caller started it.
func (c *runtimeLogCache) read(ctx context.Context, fetch func() (apprefresh.RuntimeLogs, error)) (apprefresh.RuntimeLogs, error) {
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

	// Deferred so a panic in fetch cannot leave c.inflight pointing at a read
	// that will never run again: every later caller would wait on the closed
	// channel and be served the zero snapshot forever.
	completed := false
	defer func() {
		if !completed {
			inflight.err = errRuntimeLogReadAborted
		}
		c.mu.Lock()
		c.inflight = nil
		if completed {
			c.snapshot, c.err, c.at = inflight.snapshot, inflight.err, time.Now()
		}
		c.mu.Unlock()
		close(inflight.done)
	}()

	inflight.snapshot, inflight.err = fetch()
	completed = true
	return inflight.snapshot, inflight.err
}
