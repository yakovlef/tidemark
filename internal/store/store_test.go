package store

import (
	"context"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "tidemark.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestOpenMigrates(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	v, err := s.SchemaVersion(ctx)
	if err != nil || v != 1 {
		t.Fatalf("schema version = %d, %v; want 1", v, err)
	}
	for _, table := range []string{"meta", "files", "sessions", "segments", "fts"} {
		var n int
		err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name = ?", table).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("table %s missing", table)
		}
	}
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v; want wal", mode, err)
	}
}

func TestReopenIsIdempotent(t *testing.T) {
	s, path := openTemp(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	if v, _ := s2.SchemaVersion(context.Background()); v != 1 {
		t.Fatalf("schema version after reopen = %d, want 1", v)
	}
}

func TestFTSStemsEnglish(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	_, err := s.db.ExecContext(ctx, "INSERT INTO fts(rowid, title, text) VALUES (1, 'pgx', 'migrations were running slowly')")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM fts WHERE fts MATCH 'migration run'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("stemmed match = %d, %v; want 1", n, err)
	}
}

func TestDefaultPathRespectsEnv(t *testing.T) {
	t.Setenv("TIDEMARK_DATA_DIR", "/tmp/tm")
	p, err := DefaultPath()
	if err != nil || p != "/tmp/tm/tidemark.db" {
		t.Fatalf("DefaultPath = %q, %v", p, err)
	}
}
