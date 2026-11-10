package segment

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/yakovlef/tidemark/internal/atoms"
	"github.com/yakovlef/tidemark/internal/source"
)

const (
	maxSize      = 8 << 10
	maxResult    = 2 << 10
	maxPrompt    = 1 << 10
	firstPromptN = 80
	ownTools     = "mcp__tidemark__"
)

type Session struct {
	ID          string
	Source      string
	Project     string
	Branch      string
	Title       string
	FirstPrompt string
	Start       time.Time
	End         time.Time
}

type Segment struct {
	ID        string
	SessionID string
	Seq       int
	Part      int
	Time      time.Time
	Text      string
	Atoms     []string
	Offset    int64
	Hash      string
}

type turn struct {
	prompt string
	time   time.Time
	offset int64
	lines  []string
}

func Build(src string, recs []source.Record, firstSeq int) (Session, []Segment) {
	s := Session{Source: src}
	var autoTitle string
	var turns []*turn
	for _, r := range recs {
		if s.ID == "" {
			s.ID = r.SessionID
		}
		if r.Project != "" {
			s.Project = r.Project
		}
		if r.Branch != "" {
			s.Branch = r.Branch
		}
		if !r.Time.IsZero() {
			if s.Start.IsZero() || r.Time.Before(s.Start) {
				s.Start = r.Time
			}
			if r.Time.After(s.End) {
				s.End = r.Time
			}
		}

		switch r.Kind {
		case source.Title:
			s.Title = r.Text
			continue
		case source.AutoTitle:
			autoTitle = r.Text
			continue
		case source.Prompt:
			if s.FirstPrompt == "" {
				s.FirstPrompt = truncate(strings.Join(strings.Fields(r.Text), " "), firstPromptN)
			}
			turns = append(turns, &turn{prompt: r.Text, time: r.Time, offset: r.Offset})
			continue
		}

		line := render(r)
		if line == "" {
			continue
		}
		if len(turns) == 0 {
			turns = append(turns, &turn{time: r.Time, offset: r.Offset})
		}
		t := turns[len(turns)-1]
		if t.time.IsZero() {
			t.time = r.Time
		}
		t.lines = append(t.lines, line)
	}
	if s.Title == "" {
		s.Title = autoTitle
	}

	var segs []Segment
	for i, t := range turns {
		seq := firstSeq + i
		for p, text := range split(t) {
			a := atoms.Extract(text)
			segs = append(segs, Segment{
				ID:        hash(src, s.ID, fmt.Sprint(seq), fmt.Sprint(p+1)),
				SessionID: s.ID,
				Seq:       seq,
				Part:      p + 1,
				Time:      t.time,
				Text:      text,
				Atoms:     a,
				Offset:    t.offset,
				Hash:      hash(text, strings.Join(a, "\n")),
			})
		}
	}
	return s, segs
}

func render(r source.Record) string {
	switch r.Kind {
	case source.Text:
		return r.Text
	case source.ToolUse:
		return strings.TrimSpace("$ " + r.Tool + " " + r.Text)
	case source.ToolResult:
		if strings.HasPrefix(r.Tool, ownTools) {
			return ""
		}
		return excerpt(r.Text)
	}
	return ""
}

func excerpt(text string) string {
	if len(text) <= maxResult {
		return text
	}
	head := truncate(text, maxResult)
	var errs []string
	for _, l := range strings.Split(text[len(head):], "\n") {
		if isError(l) {
			errs = append(errs, strings.TrimSpace(l))
		}
	}
	if len(errs) == 0 {
		return head + "\n…"
	}
	return head + "\n…\n" + strings.Join(errs, "\n")
}

func isError(l string) bool {
	l = strings.ToLower(l)
	for _, k := range []string{"error", "panic", "traceback", "fail", "exit code"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

func split(t *turn) []string {
	header := ""
	if t.prompt != "" {
		header = "> " + truncate(t.prompt, maxPrompt)
	}
	room := maxSize - len(header) - 1
	var parts []string
	var cur []string
	size := 0
	flush := func() {
		parts = append(parts, strings.TrimSpace(header+"\n"+strings.Join(cur, "\n")))
		cur, size = nil, 0
	}
	for _, l := range t.lines {
		l = truncate(l, room)
		if size > 0 && size+len(l)+1 > room {
			flush()
		}
		cur = append(cur, l)
		size += len(l) + 1
	}
	if len(cur) > 0 || len(parts) == 0 {
		flush()
	}
	return parts
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func hash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:])
}
