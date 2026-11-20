package main

import (
	"bytes"
	"context"
	"os"
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

func TestInit(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join("..", "..", "internal", "source", "claudecode", "testdata"))
	t.Setenv("TIDEMARK_DATA_DIR", t.TempDir())
	cfgDir := t.TempDir()
	t.Setenv("TIDEMARK_CONFIG_DIR", cfgDir)

	for _, want := range []string{"created ", "already exists"} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{"init"}, &stdout, &stderr); code != 0 {
			t.Fatalf("init = %d, stderr %q", code, stderr.String())
		}
		for _, s := range []string{want, "claudecode: 1 files", "claude mcp add tidemark -- tidemark mcp"} {
			if !strings.Contains(stdout.String(), s) {
				t.Errorf("stdout = %q, want %q", stdout.String(), s)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "config.toml")); err != nil {
		t.Error(err)
	}
}

func TestInitNoHistory(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("TIDEMARK_DATA_DIR", t.TempDir())
	t.Setenv("TIDEMARK_CONFIG_DIR", t.TempDir())

	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init = %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no agent history found") || stderr.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}
