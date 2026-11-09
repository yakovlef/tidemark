package main

import (
	"bytes"
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
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(tt.args, &stdout, &stderr); code != tt.code {
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
