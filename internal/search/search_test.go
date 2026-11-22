package search

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/yakovlef/tidemark/internal/segment"
	"github.com/yakovlef/tidemark/internal/store"
)

var now = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

type seg struct {
	text string
	age  int
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tidemark.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func addSession(t *testing.T, st *store.Store, id, src, project, title string, segs ...seg) {
	t.Helper()
	u := store.FileUpdate{
		Path:    "/logs/" + id + ".jsonl",
		Source:  src,
		Session: segment.Session{ID: id, Project: project, Title: title, Start: now, End: now},
	}
	for i, s := range segs {
		u.Segments = append(u.Segments, segment.Segment{
			ID: fmt.Sprintf("%s-%d", id, i), SessionID: id, Seq: i, Part: 1,
			Time: now.AddDate(0, 0, -s.age), Text: s.text, Hash: fmt.Sprint(i),
		})
	}
	if err := st.WriteFile(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}

func ids(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ID
	}
	return out
}

func run(t *testing.T, st *store.Store, q Query) []string {
	t.Helper()
	q.Now = now
	hits, err := Search(context.Background(), st, q)
	if err != nil {
		t.Fatal(err)
	}
	return ids(hits)
}

func TestMatch(t *testing.T) {
	terms := Terms(`pgx "migration" OR timeo*`)
	if got := Match(terms, " "); got != `"pgx" "migration" "OR" "timeo"*` {
		t.Fatalf("AND match = %s", got)
	}
	if got := Match([]string{"a", "b"}, " OR "); got != `"a" OR "b"*` {
		t.Fatalf("OR match = %s", got)
	}
}

func TestPrefixAndStemming(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "s1", "claudecode", "/p", "", seg{text: "migrations were running slowly in pgx"})

	if got := run(t, st, Query{Text: "migration pg"}); len(got) != 1 {
		t.Fatalf("hits = %v, want s1-0", got)
	}
}

func TestOrFallbackAppendsBelowAnd(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "s1", "claudecode", "/p", "", seg{text: "redis timeout in worker"})
	addSession(t, st, "s2", "claudecode", "/p", "", seg{text: "redis cluster setup"})

	got := run(t, st, Query{Text: "redis timeout"})
	if len(got) != 2 || got[0] != "s1-0" || got[1] != "s2-0" {
		t.Fatalf("hits = %v, want [s1-0 s2-0]", got)
	}
}

func TestRecencyDecay(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "old", "claudecode", "/p", "", seg{text: "deadlock in scheduler", age: 400})
	addSession(t, st, "new", "claudecode", "/p", "", seg{text: "deadlock in scheduler", age: 1})

	if got := run(t, st, Query{Text: "deadlock"}); got[0] != "new-0" {
		t.Fatalf("hits = %v, want new-0 first", got)
	}
}

func TestDecayFloor(t *testing.T) {
	if d := decay(0); d != 1 {
		t.Errorf("decay(0) = %v", d)
	}
	if d := decay(120 * 24 * time.Hour); math.Abs(d-0.5) > 1e-9 {
		t.Errorf("decay(120d) = %v, want 0.5", d)
	}
	if d := decay(5 * 365 * 24 * time.Hour); d != minDecay {
		t.Errorf("decay(5y) = %v, want %v", d, minDecay)
	}
}

func TestProjectBoost(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "other", "claudecode", "/home/dev/other", "", seg{text: "flaky test retry"})
	addSession(t, st, "shop", "claudecode", "/home/dev/shop", "", seg{text: "flaky test retry"})

	got := run(t, st, Query{Text: "flaky", CWD: "/home/dev/shop/internal"})
	if got[0] != "shop-0" {
		t.Fatalf("hits = %v, want shop-0 first", got)
	}
}

func TestTitleOutweighsText(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "body", "claudecode", "/p", "", seg{text: "we talked about webhooks and other things here"})
	addSession(t, st, "title", "claudecode", "/p", "webhooks", seg{text: "we talked about retries and other things here"})

	if got := run(t, st, Query{Text: "webhooks"}); got[0] != "title-0" {
		t.Fatalf("hits = %v, want title-0 first", got)
	}
}

func TestTwoHitsPerSession(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "s1", "claudecode", "/p", "",
		seg{text: "goroutine leak one"}, seg{text: "goroutine leak two"}, seg{text: "goroutine leak three"})
	addSession(t, st, "s2", "claudecode", "/p", "", seg{text: "goroutine leak four"})

	got := run(t, st, Query{Text: "goroutine leak"})
	if len(got) != 3 {
		t.Fatalf("hits = %v, want 2 from s1 and 1 from s2", got)
	}
}

func TestLimit(t *testing.T) {
	st := newStore(t)
	for i := range 12 {
		addSession(t, st, fmt.Sprintf("s%d", i), "claudecode", "/p", "", seg{text: "cache miss"})
	}
	if got := run(t, st, Query{Text: "cache"}); len(got) != DefaultLimit {
		t.Fatalf("default limit: %d hits", len(got))
	}
	if got := run(t, st, Query{Text: "cache", Limit: 3}); len(got) != 3 {
		t.Fatalf("limit 3: %d hits", len(got))
	}
}

func TestFilters(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "a", "claudecode", "/home/dev/shop", "", seg{text: "stripe webhook", age: 30})
	addSession(t, st, "b", "codex", "/home/dev/shop_v2", "", seg{text: "stripe webhook", age: 2})

	if got := run(t, st, Query{Text: "stripe", Source: "codex"}); len(got) != 1 || got[0] != "b-0" {
		t.Errorf("source filter = %v", got)
	}
	if got := run(t, st, Query{Text: "stripe", Project: "/home/dev/shop"}); len(got) != 1 || got[0] != "a-0" {
		t.Errorf("project filter = %v", got)
	}
	if got := run(t, st, Query{Text: "stripe", Since: now.AddDate(0, 0, -7)}); len(got) != 1 || got[0] != "b-0" {
		t.Errorf("since filter = %v", got)
	}
}

func TestQuerySyntaxIsInert(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "s1", "claudecode", "/p", "", seg{text: "near the end"})

	for _, q := range []string{`"`, `NEAR(`, `a:b`, `* -`, ``} {
		if _, err := Search(context.Background(), st, Query{Text: q, Now: now}); err != nil {
			t.Errorf("query %q: %v", q, err)
		}
	}
}

func TestSnippetAndMetadata(t *testing.T) {
	st := newStore(t)
	addSession(t, st, "s1", "claudecode", "/home/dev/shop", "checkout", seg{text: "the payment intent failed with card_declined", age: 3})

	hits, err := Search(context.Background(), st, Query{Text: "declined", Now: now})
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits = %v, %v", hits, err)
	}
	h := hits[0]
	if h.Project != "/home/dev/shop" || h.Title != "checkout" || h.Source != "claudecode" || h.Snippet == "" {
		t.Fatalf("hit = %+v", h)
	}
	if !h.Time.Equal(now.AddDate(0, 0, -3)) {
		t.Fatalf("time = %v", h.Time)
	}
}
