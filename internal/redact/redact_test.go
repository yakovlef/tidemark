package redact

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestCorpus(t *testing.T) {
	r, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/secrets.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.ReplaceAll(sc.Text(), "{{}}", "")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		secret, text, _ := strings.Cut(line, "\t")
		got := r.Redact(text)
		if strings.Contains(got, secret) || !strings.Contains(got, "[REDACTED:") {
			t.Errorf("secret survived: %q", got)
		}
	}
}

func TestKeepsSearchableValues(t *testing.T) {
	r, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"commit 89873b1c4e2f7a9d0b3c5e8f1a2b4c6d8e0f1a3b",
		"request 3f2b8c1d-9e4a-4b7c-a5d2-8c0e1f6a4d93 failed",
		"https://github.com/yakovlef/tidemark/pull/5",
		"go test ./internal/... -run TestBuild",
		"TOKEN_TTL is configured in config.go",
		"smtp_password: NotRequired[str]",
		"curl -H \"Authorization: Bearer $TOKEN\" https://api.example.com",
		"USDTOnTron(\"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t\")",
	} {
		if got := r.Redact(s); got != s {
			t.Errorf("Redact(%q) = %q", s, got)
		}
	}
}

func TestRedactsOnlyTheMatch(t *testing.T) {
	r, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	in := "AIRFLOW_CONN=postgresql://postgres:airflow@postgres:5432/airflow\nairflow.task.hooks"
	want := "AIRFLOW_CONN=postgresql://postgres:[REDACTED:dsn]@postgres:5432/airflow\nairflow.task.hooks"
	if got := r.Redact(in); got != want {
		t.Errorf("got %q", got)
	}
}

func TestCustomPatterns(t *testing.T) {
	r, err := New([]string{`(?i)acme corp`})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Redact("deploy for ACME Corp done"); got != "deploy for [REDACTED:custom] done" {
		t.Errorf("got %q", got)
	}
	if _, err := New([]string{"("}); err == nil {
		t.Error("want error for bad pattern")
	}
}

func BenchmarkRedact(b *testing.B) {
	r, err := New(nil)
	if err != nil {
		b.Fatal(err)
	}
	text := strings.Repeat("> why does the checkout test fail on CI?\n$ Bash go test ./checkout/...\n--- FAIL: TestCheckout\n", 80)
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		r.Redact(text)
	}
}
