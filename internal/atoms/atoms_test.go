package atoms

import (
	"slices"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"command", "$ Bash go test ./checkout/...", []string{"go test ./checkout/...", "./checkout/..."}},
		{"error line", "ok\n--- FAIL: TestCheckout (0.01s)\nexit code 1", []string{"--- FAIL: TestCheckout (0.01s)", "exit code 1"}},
		{"url", "see https://github.com/apache/airflow/pull/76738.", []string{"https://github.com/apache/airflow/pull/76738"}},
		{"paths", "edit internal/store/store.go and /etc/nginx/nginx.conf", []string{"internal/store/store.go", "/etc/nginx/nginx.conf"}},
		{"hash", "reverted 71a2b3c, not deadbeef words", []string{"71a2b3c"}},
		{"refs", "fixes #1538 and yakovlef/tidemark#3, ticket SHOP-42", []string{"yakovlef/tidemark#3", " #1538", "SHOP-42"}},
		{"plain prose", "the weather is nice today", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Extract(tt.text)
			want := make([]string, len(tt.want))
			for i, w := range tt.want {
				want[i] = strings.TrimSpace(w)
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("Extract(%q) = %q, want %q", tt.text, got, want)
			}
		})
	}
}

func TestExtractDedupsAndCaps(t *testing.T) {
	var b strings.Builder
	for i := range 200 {
		b.WriteString("error number ")
		b.WriteString(strings.Repeat("x", i%3))
		b.WriteString("\n")
	}
	got := Extract(b.String())
	if len(got) != 3 {
		t.Errorf("got %d atoms, want 3 unique: %q", len(got), got)
	}

	b.Reset()
	for i := range 100 {
		b.WriteString("PROJ-")
		b.WriteString(strings.Repeat("1", i+1))
		b.WriteString(" ")
	}
	if got := Extract(b.String()); len(got) != maxAtoms {
		t.Errorf("got %d atoms, want cap %d", len(got), maxAtoms)
	}
}

func TestExtractCutsLongLines(t *testing.T) {
	got := Extract("panic: " + strings.Repeat("я", 500))
	if len(got) != 1 || len([]rune(got[0])) != maxLineLen {
		t.Errorf("got %d atoms, first rune len %d", len(got), len([]rune(got[0])))
	}
}
