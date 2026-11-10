package source

import (
	"context"
	"iter"
	"time"
)

type File struct {
	Path  string
	Size  int64
	MTime time.Time
}

type Kind int

const (
	Prompt Kind = iota + 1
	Text
	ToolUse
	ToolResult
	Title
	AutoTitle
)

type Record struct {
	Kind      Kind
	SessionID string
	Time      time.Time
	Project   string
	Branch    string
	Tool      string
	Text      string
	IsError   bool
	Offset    int64
}

type Source interface {
	Name() string
	Files(ctx context.Context) ([]File, error)
	Parse(ctx context.Context, f File, from int64) iter.Seq2[Record, error]
}
