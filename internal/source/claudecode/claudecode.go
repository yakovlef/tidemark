package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yakovlef/tidemark/internal/source"
)

var (
	reminder = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	keyArgs  = []string{"command", "file_path", "path", "pattern", "query", "url", "description"}
	noise    = []string{"<command-name>", "<command-message>", "<local-command-", "<task-notification>"}
)

type Source struct{ root string }

func New(root string) *Source { return &Source{root: root} }

func DefaultRoot() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

func (s *Source) Name() string { return "claudecode" }

func (s *Source) Files(_ context.Context) ([]source.File, error) {
	paths, err := filepath.Glob(filepath.Join(s.root, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	files := make([]source.File, 0, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		files = append(files, source.File{Path: p, Size: fi.Size(), MTime: fi.ModTime()})
	}
	return files, nil
}

func (s *Source) Parse(ctx context.Context, f source.File, from int64) iter.Seq2[source.Record, error] {
	return func(yield func(source.Record, error) bool) {
		fh, err := os.Open(f.Path)
		if err != nil {
			yield(source.Record{}, err)
			return
		}
		defer func() { _ = fh.Close() }()
		if _, err := fh.Seek(from, io.SeekStart); err != nil {
			yield(source.Record{}, err)
			return
		}
		r := bufio.NewReaderSize(fh, 64<<10)
		tools := map[string]string{}
		off := from
		for {
			if err := ctx.Err(); err != nil {
				yield(source.Record{}, err)
				return
			}
			line, err := r.ReadBytes('\n')
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(source.Record{}, err)
				}
				return
			}
			start := off
			off += int64(len(line))
			for _, rec := range decode(line, tools) {
				rec.Offset = start
				if !yield(rec, nil) {
					return
				}
			}
		}
	}
}

type entry struct {
	Type             string    `json:"type"`
	SessionID        string    `json:"sessionId"`
	Timestamp        time.Time `json:"timestamp"`
	Cwd              string    `json:"cwd"`
	GitBranch        string    `json:"gitBranch"`
	IsSidechain      bool      `json:"isSidechain"`
	IsMeta           bool      `json:"isMeta"`
	IsCompactSummary bool      `json:"isCompactSummary"`
	CustomTitle      string    `json:"customTitle"`
	AITitle          string    `json:"aiTitle"`
	Message          struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func decode(line []byte, tools map[string]string) []source.Record {
	var e entry
	if json.Unmarshal(line, &e) != nil {
		return nil
	}
	base := source.Record{SessionID: e.SessionID, Time: e.Timestamp, Project: e.Cwd, Branch: e.GitBranch}
	switch e.Type {
	case "custom-title":
		return titled(base, source.Title, e.CustomTitle)
	case "ai-title":
		return titled(base, source.AutoTitle, e.AITitle)
	case "user", "assistant":
	default:
		return nil
	}
	if e.IsSidechain || e.IsMeta || e.IsCompactSummary {
		return nil
	}

	var blocks []block
	var s string
	if json.Unmarshal(e.Message.Content, &s) == nil {
		blocks = []block{{Type: "text", Text: s}}
	} else if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return nil
	}

	var out []source.Record
	var prompt []string
	for _, b := range blocks {
		rec := base
		switch b.Type {
		case "text":
			t := clean(b.Text)
			if t == "" {
				continue
			}
			if e.Type == "user" {
				prompt = append(prompt, t)
				continue
			}
			rec.Kind, rec.Text = source.Text, t
		case "tool_use":
			tools[b.ID] = b.Name
			rec.Kind, rec.Tool, rec.Text = source.ToolUse, b.Name, args(b.Input)
		case "tool_result":
			rec.Kind, rec.Tool, rec.Text, rec.IsError = source.ToolResult, tools[b.ToolUseID], clean(resultText(b.Content)), b.IsError
		default:
			continue
		}
		out = append(out, rec)
	}
	if len(prompt) > 0 {
		rec := base
		rec.Kind, rec.Text = source.Prompt, strings.Join(prompt, "\n")
		out = append([]source.Record{rec}, out...)
	}
	return out
}

func titled(base source.Record, k source.Kind, t string) []source.Record {
	t = strings.TrimSpace(t)
	if t == "" {
		return nil
	}
	base.Kind, base.Text = k, t
	return []source.Record{base}
}

func clean(t string) string {
	t = strings.TrimSpace(reminder.ReplaceAllString(t, ""))
	for _, p := range noise {
		if strings.HasPrefix(t, p) {
			return ""
		}
	}
	return t
}

func args(in map[string]any) string {
	var parts []string
	for _, k := range keyArgs {
		if v, ok := in[k].(string); ok && v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
