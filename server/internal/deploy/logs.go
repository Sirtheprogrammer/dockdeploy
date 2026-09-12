package deploy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// Build output goes two places at once: appended to deployment_logs so a
// client that reloads can replay it, and published to any watcher attached
// right now. Persisting is what makes a deploy survive a page refresh, which
// is the whole reason runs are a queue rather than a request.

// flushEvery bounds how long a line waits before being persisted. A docker
// build emits thousands of lines; one INSERT per line would make the database
// the slowest part of the build, but too large a window makes the UI feel
// stalled.
const (
	flushEvery = 250 * time.Millisecond
	flushAt    = 200
)

// Watcher receives lines as they happen.
type Watcher chan store.LogEntry

// Hub fans live log lines out to connected clients.
//
// It holds no history: replay comes from the database, and a watcher attaches
// only for what happens from now on. That split keeps the hub free of any
// buffering policy.
type Hub struct {
	mu       sync.RWMutex
	watchers map[string]map[Watcher]struct{}
}

func NewHub() *Hub {
	return &Hub{watchers: map[string]map[Watcher]struct{}{}}
}

// Watch subscribes to a run. The returned function must be called to detach.
func (h *Hub) Watch(runID string) (Watcher, func()) {
	// Buffered: a slow client must not stall the build that is producing the
	// output. Once the buffer fills, its lines are dropped rather than
	// blocking, and it can recover them by replaying from the database.
	watcher := make(Watcher, 256)

	h.mu.Lock()
	if h.watchers[runID] == nil {
		h.watchers[runID] = map[Watcher]struct{}{}
	}
	h.watchers[runID][watcher] = struct{}{}
	h.mu.Unlock()

	return watcher, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if set, ok := h.watchers[runID]; ok {
			delete(set, watcher)
			if len(set) == 0 {
				delete(h.watchers, runID)
			}
		}
		close(watcher)
	}
}

func (h *Hub) publish(runID string, entry store.LogEntry) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for watcher := range h.watchers[runID] {
		select {
		case watcher <- entry:
		default:
			// Dropped. The client replays from the database on reconnect.
		}
	}
}

// Recorder collects a run's output.
//
// It is an io.Writer per stream, so it can be handed straight to SSH command
// execution and to the Docker build API.
type Recorder struct {
	store *store.Store
	hub   *Hub
	runID string

	mu       sync.Mutex
	pending  []store.LogEntry
	nextSeq  int64
	redact   []string
	lastSent time.Time

	flushCh chan struct{}
	done    chan struct{}
	closed  bool
	wg      sync.WaitGroup
}

// NewRecorder starts a recorder for a run.
//
// redact holds values that must never reach the log: deployment secrets, git
// tokens, registry passwords. They are replaced before a line is persisted or
// published, so there is no window in which they exist in a readable form.
func NewRecorder(ctx context.Context, db *store.Store, hub *Hub, runID string, redact []string) *Recorder {
	r := &Recorder{
		store:   db,
		hub:     hub,
		runID:   runID,
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	for _, value := range redact {
		// Very short values would match everywhere and turn the log into
		// asterisks; they are also not credentials worth protecting.
		if len(value) >= 6 {
			r.redact = append(r.redact, value)
		}
	}

	r.wg.Add(1)
	go r.loop(ctx)
	return r
}

func (r *Recorder) loop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			r.flush(context.WithoutCancel(ctx))
			return
		case <-ticker.C:
			r.flush(ctx)
		case <-r.flushCh:
			r.flush(ctx)
		}
	}
}

func (r *Recorder) flush(ctx context.Context) {
	r.mu.Lock()
	if len(r.pending) == 0 {
		r.mu.Unlock()
		return
	}
	batch := r.pending
	r.pending = nil
	start := r.nextSeq - int64(len(batch))
	r.mu.Unlock()

	// A failure to persist must not abort the deploy: the build is the point,
	// and the live stream still carries the output.
	if err := r.store.AppendLogs(ctx, r.runID, start, batch); err != nil {
		// Nothing useful to do here but carry on; the caller logs at a higher
		// level when the run finishes.
		_ = err
	}
}

// Write records output on a named stream.
func (r *Recorder) Write(stream, line string) {
	for _, secret := range r.redact {
		line = strings.ReplaceAll(line, secret, "[redacted]")
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	entry := store.LogEntry{Seq: r.nextSeq, Stream: stream, Line: line, TS: time.Now().UTC()}
	r.nextSeq++
	r.pending = append(r.pending, entry)
	full := len(r.pending) >= flushAt
	r.mu.Unlock()

	r.hub.publish(r.runID, entry)

	if full {
		select {
		case r.flushCh <- struct{}{}:
		default:
		}
	}
}

// Info records a message from the platform itself rather than from a command,
// so the log reads as a narrative of what happened.
func (r *Recorder) Info(format string, args ...any) {
	r.Write("system", fmt.Sprintf(format, args...))
}

// Fail records a terminal error.
func (r *Recorder) Fail(format string, args ...any) {
	r.Write("error", fmt.Sprintf(format, args...))
}

// Stream returns an io.Writer that splits incoming bytes into lines.
func (r *Recorder) Stream(name string) io.Writer {
	return &streamWriter{recorder: r, stream: name}
}

// Close flushes everything still pending.
func (r *Recorder) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	close(r.done)
	r.wg.Wait()
}

// streamWriter turns arbitrary chunks into whole lines.
//
// Command output arrives in blocks that split lines anywhere, and a build log
// that shows half a line followed by the rest as a separate entry is much
// harder to read than one that waits for the newline.
type streamWriter struct {
	recorder *Recorder
	stream   string
	mu       sync.Mutex
	buf      strings.Builder
}

// maxLineBytes bounds a single line, so output with no newlines cannot grow
// this buffer without limit.
const maxLineBytes = 32 * 1024

func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	combined := w.buf.String() + string(p)
	w.buf.Reset()

	scanner := bufio.NewScanner(strings.NewReader(combined))
	scanner.Buffer(make([]byte, 0, 4096), maxLineBytes)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	complete := len(p) > 0 && p[len(p)-1] == '\n'
	for i, line := range lines {
		if i == len(lines)-1 && !complete {
			if len(line) >= maxLineBytes {
				w.recorder.Write(w.stream, line)
			} else {
				w.buf.WriteString(line)
			}
			continue
		}
		w.recorder.Write(w.stream, line)
	}
	return len(p), nil
}

// flushPartial emits a trailing line with no newline, at the end of a command.
func (w *streamWriter) flushPartial() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() > 0 {
		w.recorder.Write(w.stream, w.buf.String())
		w.buf.Reset()
	}
}
