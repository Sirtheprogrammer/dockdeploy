package sshx

import (
	"context"
	"io/fs"
	"log/slog"
	"sync"
)

// fs converts a numeric mode for SFTP chmod.
func fsMode(mode uint32) fs.FileMode { return fs.FileMode(mode) }

// Pool keeps one SSH connection per server and shares it.
//
// A fresh SSH handshake costs hundreds of milliseconds, and a single page of
// the dashboard makes several Docker API calls. Without pooling, listing
// containers on three servers would open a dozen connections and feel broken.
type Pool struct {
	log *slog.Logger

	mu      sync.Mutex
	entries map[string]*entry
	closed  bool
}

type entry struct {
	conn *Conn
	// refs counts live borrowers. The connection is kept open at zero refs so
	// the next request reuses it; only Evict and Close actually tear it down.
	refs int
}

func NewPool(log *slog.Logger) *Pool {
	return &Pool{log: log, entries: map[string]*entry{}}
}

// Get returns a connection for a server, dialling only if there is not already
// a healthy one.
//
// key identifies the server (its row id). It is separate from Target so that
// changing a server's address or credentials evicts the old connection rather
// than silently reusing it.
func (p *Pool) Get(ctx context.Context, key string, target Target, cred Credential) (*Conn, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, context.Canceled
	}
	if existing, ok := p.entries[key]; ok {
		existing.refs++
		p.mu.Unlock()
		return &Conn{client: existing.conn.client, pool: p, key: key}, nil
	}
	p.mu.Unlock()

	// Dial outside the lock: a slow or unreachable host must not block calls
	// to every other server.
	client, err := Connect(ctx, target, cred)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		_ = client.Close()
		return nil, context.Canceled
	}

	// Another goroutine may have dialled the same server while this one was
	// waiting. Keep the first and discard this one rather than leaking it.
	if existing, ok := p.entries[key]; ok {
		_ = client.Close()
		existing.refs++
		return &Conn{client: existing.conn.client, pool: p, key: key}, nil
	}

	created := &Conn{client: client, pool: p, key: key}
	p.entries[key] = &entry{conn: created, refs: 1}
	p.log.Debug("ssh connection opened", "server", key, "host", target.Host)

	// A connection dropped by the far end must not stay in the map, or every
	// later request gets handed a dead client.
	go func() {
		_ = client.Wait()
		p.discard(key, client)
	}()

	return &Conn{client: client, pool: p, key: key}, nil
}

func (p *Pool) release(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[key]; ok && e.refs > 0 {
		e.refs--
	}
}

// discard removes a specific dead client, leaving any replacement in place.
func (p *Pool) discard(key string, client interface{ Close() error }) {
	p.mu.Lock()
	defer p.mu.Unlock()

	e, ok := p.entries[key]
	if !ok || e.conn.client != client {
		return
	}
	delete(p.entries, key)
	p.log.Debug("ssh connection closed", "server", key)
}

// Evict drops a server's connection. Call it whenever the credentials, address
// or pinned host key change, so the next request reconnects with the new
// details instead of reusing a connection made under the old ones.
func (p *Pool) Evict(key string) {
	p.mu.Lock()
	e, ok := p.entries[key]
	if ok {
		delete(p.entries, key)
	}
	p.mu.Unlock()

	if ok {
		_ = e.conn.client.Close()
		p.log.Debug("ssh connection evicted", "server", key)
	}
}

// Close tears down every connection, for graceful shutdown.
func (p *Pool) Close() {
	p.mu.Lock()
	entries := p.entries
	p.entries = map[string]*entry{}
	p.closed = true
	p.mu.Unlock()

	for key, e := range entries {
		_ = e.conn.client.Close()
		p.log.Debug("ssh connection closed on shutdown", "server", key)
	}
}

// Len reports how many connections are open, for the health endpoint.
func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}
