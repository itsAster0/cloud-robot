// Package logbuf keeps recent server and worker log entries in memory so the
// admin console can show them live. It is bounded and loses history on
// restart; durable logs remain the process's stdout/stderr.
package logbuf

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Seq     uint64            `json:"seq"`
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Source  string            `json:"source"`
	MatchID string            `json:"matchId,omitempty"`
	Message string            `json:"message"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

type Buffer struct {
	mu      sync.Mutex
	entries []Entry
	next    int
	full    bool
	seq     uint64
}

func New(capacity int) *Buffer {
	return &Buffer{entries: make([]Entry, capacity)}
}

func (b *Buffer) Add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e.Seq = b.seq
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b.entries[b.next] = e
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
}

type Query struct {
	After   uint64 // only entries with Seq > After
	Source  string // exact source, or prefix ending in ':' e.g. "worker:"
	MatchID string
	Level   string // minimum level: DEBUG, INFO, WARN, ERROR
	Search  string // case-insensitive substring of message or attrs
	Limit   int
}

var levelRank = map[string]int{"DEBUG": 0, "INFO": 1, "WARN": 2, "ERROR": 3}

// Find returns matching entries in order, newest last, at most q.Limit of
// the most recent ones, plus the latest sequence number for polling.
func (b *Buffer) Find(q Query) ([]Entry, uint64) {
	b.mu.Lock()
	var ordered []Entry
	if b.full {
		ordered = append(append(ordered, b.entries[b.next:]...), b.entries[:b.next]...)
	} else {
		ordered = append(ordered, b.entries[:b.next]...)
	}
	latest := b.seq
	b.mu.Unlock()
	limit := q.Limit
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	search := strings.ToLower(q.Search)
	out := []Entry{}
	for _, e := range ordered {
		if e.Seq <= q.After ||
			(q.Source != "" && e.Source != q.Source && !(strings.HasSuffix(q.Source, ":") && strings.HasPrefix(e.Source, q.Source))) ||
			(q.MatchID != "" && e.MatchID != q.MatchID) ||
			(q.Level != "" && levelRank[e.Level] < levelRank[strings.ToUpper(q.Level)]) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(e.Message+" "+fmt.Sprint(e.Attrs)), search) {
			continue
		}
		out = append(out, e)
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, latest
}

// Handler tees slog records to another handler and into the buffer as
// source "api". A "match" attribute becomes the entry's MatchID.
type Handler struct {
	next  slog.Handler
	buf   *Buffer
	attrs []slog.Attr
}

func NewHandler(next slog.Handler, buf *Buffer) *Handler { return &Handler{next: next, buf: buf} }

// Enabled captures INFO and above for the console even when the wrapped
// handler is quieter, plus whatever the wrapped handler wants.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo || h.next.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	e := Entry{Time: r.Time.UTC(), Level: r.Level.String(), Source: "api", Message: r.Message, Attrs: map[string]string{}}
	add := func(a slog.Attr) bool {
		value := a.Value.Resolve().String()
		if a.Key == "match" || a.Key == "matchId" {
			e.MatchID = value
		} else if a.Key == "source" {
			e.Source = value
		} else {
			e.Attrs[a.Key] = value
		}
		return true
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)
	if len(e.Attrs) == 0 {
		e.Attrs = nil
	}
	h.buf.Add(e)
	if !h.next.Enabled(ctx, r.Level) {
		return nil
	}
	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), buf: h.buf, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), buf: h.buf, attrs: h.attrs}
}

// LineWriter returns a writer that copies to `also` and records each line
// as an entry from `source` for `matchID`. Lines mentioning panic or error
// are recorded at ERROR, others at INFO. Close it after the process exits.
func (b *Buffer) LineWriter(source, matchID string, also io.Writer) io.WriteCloser {
	reader, writer := io.Pipe()
	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if also != nil {
				fmt.Fprintln(also, line)
			}
			level := "INFO"
			if lower := strings.ToLower(line); strings.Contains(lower, "panic") || strings.Contains(lower, "error") {
				level = "ERROR"
			}
			b.Add(Entry{Level: level, Source: source, MatchID: matchID, Message: line})
		}
		_ = reader.Close()
	}()
	return writer
}
