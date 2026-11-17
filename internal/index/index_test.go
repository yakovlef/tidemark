package index

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yakovlef/tidemark/internal/source/claudecode"
	"github.com/yakovlef/tidemark/internal/store"
)

const sid = "bbbb2222-0000-4000-8000-000000000001"

type env struct {
	t      *testing.T
	root   string
	file   string
	dbPath string
	st     *store.Store
	db     *sql.DB
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{t: t, root: filepath.Join(dir, "projects"), dbPath: filepath.Join(dir, "tidemark.db")}
	e.file = filepath.Join(e.root, "-home-dev-shop", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(e.file), 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	e.st, e.db = st, db
	t.Cleanup(func() { _ = db.Close(); _ = st.Close() })
	return e
}

func line(kind, content string, sec int) string {
	return fmt.Sprintf(`{"type":%q,"sessionId":%q,"cwd":"/home/dev/shop","gitBranch":"main","timestamp":"2026-10-01T10:00:%02dZ","message":{"content":%s}}`+"\n",
		kind, sid, sec, content)
}

func prompt(text string, sec int) string { return line("user", fmt.Sprintf("%q", text), sec) }

func reply(text string, sec int) string {
	return line("assistant", fmt.Sprintf(`[{"type":"text","text":%q}]`, text), sec)
}

func (e *env) write(s string) {
	e.t.Helper()
	if err := os.WriteFile(e.file, []byte(s), 0o600); err != nil {
		e.t.Fatal(err)
	}
	e.touch()
}

func (e *env) append(s string) {
	e.t.Helper()
	f, err := os.OpenFile(e.file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		e.t.Fatal(err)
	}
	_ = f.Close()
	e.touch()
}

// touch moves mtime forward so a rewrite of the same size still looks changed.
func (e *env) touch() {
	e.t.Helper()
	fi, err := os.Stat(e.file)
	if err != nil {
		e.t.Fatal(err)
	}
	next := fi.ModTime().Add(time.Second)
	if err := os.Chtimes(e.file, next, next); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) sync(opt Options) Stats {
	e.t.Helper()
	stats, err := Sync(context.Background(), e.st, claudecode.New(e.root), opt)
	if err != nil {
		e.t.Fatal(err)
	}
	return stats
}

func (e *env) texts() []string {
	e.t.Helper()
	rows, err := e.db.Query("SELECT text FROM segments ORDER BY seq, part")
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func (e *env) count(q string) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(q).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestSyncIncremental(t *testing.T) {
	e := newEnv(t)
	e.write(prompt("why does checkout fail?", 0) + reply("CI runs in UTC.", 1))

	s := e.sync(Options{})
	if s.Files != 1 || s.Changed != 1 || s.Segments != 1 {
		t.Fatalf("first sync = %+v", s)
	}
	if got := e.count("SELECT count(*) FROM fts WHERE fts MATCH 'checkout'"); got != 1 {
		t.Errorf("fts hits = %d", got)
	}
	var title string
	if err := e.db.QueryRow("SELECT title FROM sessions WHERE id = ?", sid).Scan(&title); err != nil || title != "why does checkout fail?" {
		t.Errorf("title = %q, %v", title, err)
	}

	if s := e.sync(Options{}); s.Changed != 0 {
		t.Errorf("unchanged file re-read: %+v", s)
	}

	e.append(reply("Set TZ in the test.", 2) + prompt("now fix the linter", 3))
	if s := e.sync(Options{}); s.Changed != 1 || s.Segments != 2 {
		t.Fatalf("append sync = %+v", s)
	}
	want := []string{
		"> why does checkout fail?\nCI runs in UTC.\nSet TZ in the test.",
		"> now fix the linter",
	}
	if got := e.texts(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("segments = %q", got)
	}
	if got := e.count("SELECT count(*) FROM fts"); got != 2 {
		t.Errorf("fts rows = %d, want 2", got)
	}
	if got := e.count("SELECT count(*) FROM fts WHERE fts MATCH 'title:checkout'"); got != 2 {
		t.Errorf("title not copied to every segment: %d", got)
	}
}

func TestSyncRereadsRewrittenFile(t *testing.T) {
	e := newEnv(t)
	e.write(prompt("first question", 0) + reply("first answer", 1) + prompt("second question", 2))
	e.sync(Options{})

	e.write(prompt("other question", 0))
	if s := e.sync(Options{}); s.Changed != 1 {
		t.Fatalf("sync = %+v", s)
	}
	if got := e.texts(); len(got) != 1 || got[0] != "> other question" {
		t.Errorf("segments = %q", got)
	}
	if got := e.count("SELECT count(*) FROM fts"); got != 1 {
		t.Errorf("fts rows = %d", got)
	}
}

func TestSyncRedacts(t *testing.T) {
	e := newEnv(t)
	e.write(prompt("connect with\nDB_PASSWORD=hunter2hunter2\nto acme-prod", 0) +
		line("assistant", `[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"psql postgres://app:s3cretpass@db/app"}}]`, 1))
	e.sync(Options{Patterns: []string{`acme-\w+`}})

	var all strings.Builder
	rows, err := e.db.Query("SELECT text, atoms FROM segments UNION ALL SELECT title, '' FROM sessions")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		all.WriteString(a + "\n" + b + "\n")
	}
	_ = rows.Close()
	for _, secret := range []string{"hunter2hunter2", "s3cretpass", "acme-prod"} {
		if strings.Contains(all.String(), secret) {
			t.Errorf("%q survived:\n%s", secret, all.String())
		}
	}
	if !strings.Contains(all.String(), "[REDACTED:custom]") || !strings.Contains(all.String(), "[REDACTED:dsn]") {
		t.Errorf("no redaction markers:\n%s", all.String())
	}
}

func TestSyncReindexesWhenRulesChange(t *testing.T) {
	e := newEnv(t)
	e.write(prompt("deploy acme-prod", 0))
	e.sync(Options{})
	if got := e.texts(); got[0] != "> deploy acme-prod" {
		t.Fatalf("segments = %q", got)
	}

	s := e.sync(Options{Patterns: []string{`acme-\w+`}})
	if !s.Reindex || s.Changed != 1 {
		t.Fatalf("sync = %+v", s)
	}
	if got := e.texts(); len(got) != 1 || got[0] != "> deploy [REDACTED:custom]" {
		t.Errorf("segments = %q", got)
	}
	if got := e.count("SELECT count(*) FROM fts WHERE fts MATCH 'acme'"); got != 0 {
		t.Errorf("old text still in fts: %d", got)
	}
}

func TestSyncReindexFlag(t *testing.T) {
	e := newEnv(t)
	e.write(prompt("hello", 0))
	e.sync(Options{})
	if s := e.sync(Options{Reindex: true}); !s.Reindex || s.Changed != 1 {
		t.Fatalf("sync = %+v", s)
	}
	if got := e.count("SELECT count(*) FROM segments"); got != 1 {
		t.Errorf("segments = %d", got)
	}
}
