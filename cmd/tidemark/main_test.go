package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		args       []string
		code       int
		wantStdout string
		wantStderr string
	}{
		{nil, 2, "", "Usage:"},
		{[]string{"version"}, 0, "dev\n", ""},
		{[]string{"help"}, 0, "Usage:", ""},
		{[]string{"nope"}, 2, "", `unknown command "nope"`},
		{[]string{"sync", "--source", "codex"}, 2, "", `unknown source "codex"`},
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), tt.args, &stdout, &stderr); code != tt.code {
			t.Errorf("run(%v) = %d, want %d", tt.args, code, tt.code)
		}
		if !strings.Contains(stdout.String(), tt.wantStdout) {
			t.Errorf("run(%v) stdout = %q, want %q", tt.args, stdout.String(), tt.wantStdout)
		}
		if !strings.Contains(stderr.String(), tt.wantStderr) {
			t.Errorf("run(%v) stderr = %q, want %q", tt.args, stderr.String(), tt.wantStderr)
		}
	}
}

func TestSync(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join("..", "..", "internal", "source", "claudecode", "testdata"))
	t.Setenv("TIDEMARK_DATA_DIR", t.TempDir())
	t.Setenv("TIDEMARK_CONFIG_DIR", t.TempDir())

	for _, want := range []string{"1 files, 1 changed", "1 files, 0 changed"} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{"sync"}, &stdout, &stderr); code != 0 {
			t.Fatalf("sync = %d, stderr %q", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "claudecode: "+want) {
			t.Errorf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}
