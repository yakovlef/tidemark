package claudecode

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yakovlef/tidemark/internal/source"
)

const fixture = "testdata/projects/-home-dev-shop/aaaa1111-0000-4000-8000-000000000001.jsonl"

type got struct {
	Kind    source.Kind
	Tool    string
	Text    string
	IsError bool
}

func parse(t *testing.T, from int64) []source.Record {
	t.Helper()
	var recs []source.Record
	for rec, err := range New("testdata/projects").Parse(context.Background(), source.File{Path: fixture}, from) {
		if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func TestFilesSkipsSubagents(t *testing.T) {
	files, err := New("testdata/projects").Files(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != filepath.FromSlash(fixture) || files[0].Size == 0 {
		t.Fatalf("files = %+v", files)
	}
}

func TestFilesMissingRoot(t *testing.T) {
	files, err := New(filepath.Join(t.TempDir(), "nope")).Files(context.Background())
	if err != nil || len(files) != 0 {
		t.Fatalf("files = %v, err = %v", files, err)
	}
}

func TestParse(t *testing.T) {
	want := []got{
		{Kind: source.Prompt, Text: "why does the checkout test fail on CI?"},
		{Kind: source.ToolUse, Tool: "Bash", Text: "go test ./checkout/..."},
		{Kind: source.ToolResult, Tool: "Bash", Text: "--- FAIL: TestCheckout (0.01s)\nexit code 1", IsError: true},
		{Kind: source.Text, Text: "The test reads TZ from the environment, CI runs in UTC."},
		{Kind: source.Title, Text: "checkout TZ flake"},
		{Kind: source.AutoTitle, Text: "Fix checkout test"},
		{Kind: source.Prompt, Text: "search the docs for TZ"},
		{Kind: source.ToolUse, Tool: "mcp__tidemark__search", Text: "TZ checkout"},
		{Kind: source.ToolResult, Tool: "mcp__tidemark__search", Text: "1 hit"},
	}
	recs := parse(t, 0)
	if len(recs) != len(want) {
		t.Fatalf("got %d records, want %d: %+v", len(recs), len(want), recs)
	}
	for i, r := range recs {
		if g := (got{r.Kind, r.Tool, r.Text, r.IsError}); g != want[i] {
			t.Errorf("record %d = %+v, want %+v", i, g, want[i])
		}
	}
	first := recs[0]
	if first.SessionID != "aaaa1111-0000-4000-8000-000000000001" || first.Project != "/home/dev/shop" || first.Branch != "main" || first.Time.IsZero() {
		t.Errorf("first record meta = %+v", first)
	}
}

func TestParseFromOffset(t *testing.T) {
	all := parse(t, 0)
	second := all[6]
	recs := parse(t, second.Offset)
	if len(recs) != 3 || recs[0] != second {
		t.Fatalf("from %d got %+v", second.Offset, recs)
	}
	if recs[2].Tool != "mcp__tidemark__search" {
		t.Errorf("result tool = %q", recs[2].Tool)
	}
}

func TestParseCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range New("").Parse(ctx, source.File{Path: fixture}, 0) {
		if err == nil {
			t.Fatal("want context error")
		}
		return
	}
	t.Fatal("no records")
}
