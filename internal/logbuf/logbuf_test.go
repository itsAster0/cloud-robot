package logbuf

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestBufferKeepsNewestAndFilters(t *testing.T) {
	b := New(3)
	for i, source := range []string{"api", "worker:m1", "worker:m2", "api"} {
		b.Add(Entry{Level: []string{"INFO", "ERROR", "INFO", "WARN"}[i], Source: source, MatchID: map[bool]string{true: "m1"}[i == 1], Message: "line"})
	}
	all, latest := b.Find(Query{})
	if latest != 4 || len(all) != 3 || all[0].Seq != 2 || all[2].Seq != 4 {
		t.Fatalf("ring kept wrong entries: %+v latest %d", all, latest)
	}
	if got, _ := b.Find(Query{Source: "worker:"}); len(got) != 2 {
		t.Fatalf("prefix source filter: %+v", got)
	}
	if got, _ := b.Find(Query{Level: "warn"}); len(got) != 2 {
		t.Fatalf("level filter: %+v", got)
	}
	if got, _ := b.Find(Query{MatchID: "m1"}); len(got) != 1 {
		t.Fatalf("match filter: %+v", got)
	}
	if got, _ := b.Find(Query{After: 3}); len(got) != 1 || got[0].Seq != 4 {
		t.Fatalf("after filter: %+v", got)
	}
}

func TestHandlerRecordsMatchAttribute(t *testing.T) {
	b := New(10)
	var out bytes.Buffer
	logger := slog.New(NewHandler(slog.NewTextHandler(&out, nil), b)).With("component", "worker")
	logger.Info("match started", "match", "abc", "robots", 4)
	got, _ := b.Find(Query{MatchID: "abc"})
	if len(got) != 1 || got[0].Attrs["robots"] != "4" || got[0].Attrs["component"] != "worker" || out.Len() == 0 {
		t.Fatalf("handler entry: %+v, stdout %q", got, out.String())
	}
}

func TestLineWriterSplitsLines(t *testing.T) {
	b := New(10)
	var also bytes.Buffer
	w := b.LineWriter("worker:m", "m", &also)
	_, _ = io.WriteString(w, "ready\nthread panicked at x\n")
	_ = w.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got, _ := b.Find(Query{}); len(got) == 2 {
			if got[1].Level != "ERROR" || got[0].MatchID != "m" {
				t.Fatalf("line entries: %+v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("lines not recorded")
}
