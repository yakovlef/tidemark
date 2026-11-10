package segment

import (
	"strings"
	"testing"
	"time"

	"github.com/yakovlef/tidemark/internal/source"
)

var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func rec(k source.Kind, tool, text string, sec int) source.Record {
	return source.Record{Kind: k, SessionID: "s1", Project: "/home/dev/shop", Branch: "main", Tool: tool, Text: text, Time: t0.Add(time.Duration(sec) * time.Second), Offset: int64(sec * 100)}
}

func TestBuild(t *testing.T) {
	recs := []source.Record{
		{Kind: source.AutoTitle, SessionID: "s1", Text: "Fix checkout test"},
		rec(source.Prompt, "", "why does the checkout test fail on CI?", 0),
		rec(source.ToolUse, "Bash", "go test ./checkout/...", 1),
		rec(source.ToolResult, "Bash", "--- FAIL: TestCheckout\nexit code 1", 2),
		rec(source.Text, "", "CI runs in UTC.", 3),
		rec(source.Prompt, "", "search old sessions", 10),
		rec(source.ToolUse, "mcp__tidemark__search", "TZ", 11),
		rec(source.ToolResult, "mcp__tidemark__search", "1 hit", 12),
	}
	s, segs := Build("claudecode", recs, 0)

	if s.ID != "s1" || s.Title != "Fix checkout test" || s.FirstPrompt != "why does the checkout test fail on CI?" ||
		s.Project != "/home/dev/shop" || !s.Start.Equal(t0) || !s.End.Equal(t0.Add(12*time.Second)) {
		t.Fatalf("session = %+v", s)
	}
	if len(segs) != 2 {
		t.Fatalf("got %d segments", len(segs))
	}
	want0 := "> why does the checkout test fail on CI?\n$ Bash go test ./checkout/...\n--- FAIL: TestCheckout\nexit code 1\nCI runs in UTC."
	if segs[0].Text != want0 || segs[0].Seq != 0 || segs[0].Part != 1 || segs[0].Offset != 0 {
		t.Errorf("segment 0 = %+v", segs[0])
	}
	if !contains(segs[0].Atoms, "go test ./checkout/...") || !contains(segs[0].Atoms, "exit code 1") {
		t.Errorf("atoms = %q", segs[0].Atoms)
	}
	if strings.Contains(segs[1].Text, "1 hit") || !strings.Contains(segs[1].Text, "$ mcp__tidemark__search TZ") {
		t.Errorf("own tool result not dropped: %q", segs[1].Text)
	}
	if segs[1].Seq != 1 || segs[1].Offset != 1000 {
		t.Errorf("segment 1 = %+v", segs[1])
	}
}

func TestBuildTitlePriority(t *testing.T) {
	recs := []source.Record{
		{Kind: source.Title, SessionID: "s1", Text: "mine"},
		{Kind: source.AutoTitle, SessionID: "s1", Text: "auto"},
		rec(source.Prompt, "", "hi", 0),
	}
	if s, _ := Build("claudecode", recs, 0); s.Title != "mine" {
		t.Errorf("title = %q", s.Title)
	}
}

func TestBuildOrphanRecords(t *testing.T) {
	recs := []source.Record{
		rec(source.Text, "", "continuing after compact", 0),
		rec(source.Prompt, "", "next", 5),
	}
	_, segs := Build("claudecode", recs, 7)
	if len(segs) != 2 || segs[0].Text != "continuing after compact" || segs[0].Seq != 7 || segs[1].Seq != 8 {
		t.Fatalf("segments = %+v", segs)
	}
}

func TestBuildSplitsLongTurns(t *testing.T) {
	recs := []source.Record{rec(source.Prompt, "", "refactor everything", 0)}
	for i := range 30 {
		recs = append(recs, rec(source.Text, "", strings.Repeat("word ", 100), i+1))
	}
	_, segs := Build("claudecode", recs, 0)
	if len(segs) < 2 {
		t.Fatalf("got %d segments, want split", len(segs))
	}
	ids := map[string]bool{}
	for i, s := range segs {
		if len(s.Text) > maxSize {
			t.Errorf("part %d is %d bytes", s.Part, len(s.Text))
		}
		if !strings.HasPrefix(s.Text, "> refactor everything\n") || s.Part != i+1 || s.Seq != 0 {
			t.Errorf("part %d = seq %d, prefix %q", s.Part, s.Seq, s.Text[:30])
		}
		ids[s.ID] = true
	}
	if len(ids) != len(segs) {
		t.Error("part ids are not unique")
	}
}

func TestBuildTruncatesResults(t *testing.T) {
	long := strings.Repeat("ok line\n", 1000) + "panic: boom\n" + strings.Repeat("ok\n", 10)
	_, segs := Build("claudecode", []source.Record{
		rec(source.Prompt, "", "run", 0),
		rec(source.ToolResult, "Bash", long, 1),
	}, 0)
	text := segs[0].Text
	if len(text) > maxResult+200 || !strings.HasSuffix(text, "panic: boom") {
		t.Errorf("excerpt is %d bytes, tail %q", len(text), text[len(text)-40:])
	}
}

func TestBuildIsStable(t *testing.T) {
	recs := []source.Record{rec(source.Prompt, "", "hi", 0), rec(source.Text, "", "hello", 1)}
	_, a := Build("claudecode", recs, 0)
	_, b := Build("claudecode", recs, 0)
	if a[0].ID != b[0].ID || a[0].Hash != b[0].Hash {
		t.Error("ids or hashes differ between runs")
	}
	recs[1].Text = "hello again"
	_, c := Build("claudecode", recs, 0)
	if c[0].ID != a[0].ID || c[0].Hash == a[0].Hash {
		t.Error("id should stay and hash should change when text changes")
	}
}

func TestTruncateKeepsRunes(t *testing.T) {
	if got := truncate("приветик", 5); got != "пр" {
		t.Errorf("truncate = %q", got)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
