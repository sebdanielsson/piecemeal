// Package logbuf keeps the most recent log records in memory, for the log
// endpoints of both APIs.
package logbuf

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// LogEntry is one log record.
type LogEntry struct {
	ID      int64     `json:"id"`
	Time    time.Time `json:"time"`
	Level   string    `json:"level" enum:"DEBUG,INFO,WARN,ERROR"`
	Message string    `json:"message"`
	// Attrs is the record's attributes as key=value pairs.
	Attrs string `json:"attrs"`
}

// Buffer is a ring buffer of log entries.
type Buffer struct {
	mu      sync.Mutex
	entries []LogEntry
	next    int
	full    bool
	lastID  int64
}

// New returns a buffer holding up to size entries.
func New(size int) *Buffer {
	return &Buffer{entries: make([]LogEntry, size)}
}

func (b *Buffer) add(e LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastID++
	e.ID = b.lastID
	b.entries[b.next] = e
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
}

// Since returns entries with IDs above after, oldest first, at the given
// minimum level.
func (b *Buffer) Since(after int64, minLevel slog.Level) []LogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var all []LogEntry
	if b.full {
		all = append(all, b.entries[b.next:]...)
	}
	all = append(all, b.entries[:b.next]...)
	out := []LogEntry{}
	for _, e := range all {
		var l slog.Level
		_ = l.UnmarshalText([]byte(e.Level))
		if e.ID > after && l >= minLevel {
			out = append(out, e)
		}
	}
	return out
}

// Handler returns a slog handler that records into the buffer at info
// level and above, and passes records on to next.
func (b *Buffer) Handler(next slog.Handler) slog.Handler {
	return &handler{buf: b, next: next}
}

type handler struct {
	buf    *Buffer
	next   slog.Handler
	attrs  []slog.Attr
	groups []string
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelInfo || h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	var sb strings.Builder
	prefix := strings.Join(h.groups, ".")
	write := func(a slog.Attr) {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		k := a.Key
		if prefix != "" {
			k = prefix + "." + k
		}
		fmt.Fprintf(&sb, "%s=%v", k, a.Value.Resolve())
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(func(a slog.Attr) bool { write(a); return true })
	if r.Level >= slog.LevelInfo {
		h.buf.add(LogEntry{Time: r.Time, Level: r.Level.String(), Message: r.Message, Attrs: sb.String()})
	}
	if !h.next.Enabled(ctx, r.Level) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{buf: h.buf, next: h.next.WithAttrs(attrs), attrs: append(append([]slog.Attr{}, h.attrs...), attrs...), groups: h.groups}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{buf: h.buf, next: h.next.WithGroup(name), attrs: h.attrs, groups: append(append([]string{}, h.groups...), name)}
}
