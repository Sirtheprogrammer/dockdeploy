package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
)

// Logs are delivered as Server-Sent Events rather than a websocket.
//
// The traffic is one-directional and text-only, EventSource reconnects on its
// own, and it rides the existing session cookie with no extra auth handshake.
// A websocket buys nothing here; the interactive terminal in a later milestone
// is where one is actually required.

// logLine is one framed line sent to the browser. Marking the stream lets the
// client tint stderr without guessing.
type logLine struct {
	Stream string `json:"stream"` // "stdout" or "stderr"
	Text   string `json:"text"`
}

// handleContainerLogs streams a container's output.
//
// Query parameters: tail (default 200), since, follow (default true).
func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return Internal(fmt.Errorf("api: response writer does not support flushing"))
	}

	session, server, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	query := r.URL.Query()
	opts := dockerLogOptions(query)
	containerID := chi.URLParam(r, "containerID")

	// Confirm the container exists before committing to a 200 and an event
	// stream, so a bad id is a clean 404 rather than an empty stream.
	if _, err := session.Docker.InspectContainer(r.Context(), containerID); err != nil {
		return NotFound("No such container on this server.").WithCause(err)
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	// Defeats proxy buffering, which otherwise holds the stream until it has
	// enough bytes and makes live logs look broken.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writer := &eventWriter{w: w, flusher: flusher}
	stdout := &lineWriter{stream: "stdout", out: writer}
	stderr := &lineWriter{stream: "stderr", out: writer}

	s.Log.Debug("log stream opened",
		"server", server.Name, "container", containerID,
		"actor", MustIdentity(r.Context()).User.Email)

	// Logs blocks until the client disconnects or the container stops. The
	// request context cancels on disconnect, which unwinds the Engine stream.
	if err := session.Docker.Logs(r.Context(), containerID, opts, stdout, stderr); err != nil {
		if r.Context().Err() == nil {
			s.Log.Warn("log stream failed", "container", containerID, "error", err)
			writer.event("error", map[string]string{"message": "The log stream ended unexpectedly."})
		}
	}

	stdout.flush()
	stderr.flush()
	writer.event("end", map[string]string{"reason": "closed"})

	// The response is already fully written; returning nil keeps Wrap from
	// trying to append an error envelope to a committed stream.
	return nil
}

func dockerLogOptions(query map[string][]string) (opts dockerx.LogOptions) {
	opts.Follow = true
	if values, ok := query["follow"]; ok && len(values) > 0 {
		opts.Follow = values[0] != "false" && values[0] != "0"
	}
	if values, ok := query["tail"]; ok && len(values) > 0 && values[0] != "" {
		opts.Tail = values[0]
	}
	if values, ok := query["since"]; ok && len(values) > 0 {
		opts.Since = values[0]
	}
	return opts
}

// eventWriter serialises SSE frames. Docker writes stdout and stderr from two
// goroutines inside StdCopy, so the mutex is load-bearing: without it frames
// interleave and the client sees corrupt JSON.
type eventWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

func (e *eventWriter) event(name string, payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if name != "" {
		_, _ = fmt.Fprintf(e.w, "event: %s\n", name)
	}
	_, _ = fmt.Fprintf(e.w, "data: %s\n\n", encoded)
	e.flusher.Flush()
}

// lineWriter buffers partial reads and emits one event per complete line.
//
// The Engine hands over arbitrary chunks, which may split a line anywhere. A
// naive writer would send half a line and then the rest, and the log pane
// would show them as two entries.
type lineWriter struct {
	stream string
	out    *eventWriter
	buf    strings.Builder
}

// maxLineBytes stops a container printing an unbounded line without newlines
// from growing this buffer until the process runs out of memory.
const maxLineBytes = 64 * 1024

func (l *lineWriter) Write(p []byte) (int, error) {
	scanner := bufio.NewScanner(strings.NewReader(l.buf.String() + string(p)))
	scanner.Buffer(make([]byte, 0, 4096), maxLineBytes)
	l.buf.Reset()

	// Whether the final segment is a complete line decides if it is emitted or
	// held back for the next chunk.
	complete := len(p) > 0 && p[len(p)-1] == '\n'

	var pending []string
	for scanner.Scan() {
		pending = append(pending, scanner.Text())
	}

	for i, line := range pending {
		last := i == len(pending)-1
		if last && !complete {
			if len(line) > maxLineBytes {
				// Give up on finding a newline and flush what we have.
				l.out.event("log", logLine{Stream: l.stream, Text: line})
				continue
			}
			l.buf.WriteString(line)
			continue
		}
		l.out.event("log", logLine{Stream: l.stream, Text: line})
	}

	return len(p), nil
}

// flush emits any trailing partial line when the stream ends.
func (l *lineWriter) flush() {
	if l.buf.Len() > 0 {
		l.out.event("log", logLine{Stream: l.stream, Text: l.buf.String()})
		l.buf.Reset()
	}
}

// ensure lineWriter satisfies io.Writer.
var _ io.Writer = (*lineWriter)(nil)
