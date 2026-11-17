package index

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/yakovlef/tidemark/internal/redact"
	"github.com/yakovlef/tidemark/internal/segment"
	"github.com/yakovlef/tidemark/internal/source"
	"github.com/yakovlef/tidemark/internal/store"
)

type Options struct {
	Patterns []string
	Reindex  bool
	Progress func(done, total int)
}

type Stats struct {
	Files    int
	Changed  int
	Segments int
	Reindex  bool
}

func Sync(ctx context.Context, st *store.Store, src source.Source, opt Options) (Stats, error) {
	var stats Stats
	red, err := redact.New(opt.Patterns)
	if err != nil {
		return stats, err
	}

	rulesKey := "redact_rules:" + src.Name()
	rules := rulesVersion(opt.Patterns)
	stored, err := st.Meta(ctx, rulesKey)
	if err != nil {
		return stats, err
	}
	stats.Reindex = opt.Reindex || (stored != "" && stored != rules)
	if stats.Reindex {
		if err := st.Reset(ctx, src.Name()); err != nil {
			return stats, err
		}
	}

	files, err := src.Files(ctx)
	if err != nil {
		return stats, err
	}
	stats.Files = len(files)
	for i, f := range files {
		n, changed, err := syncFile(ctx, st, src, red, f)
		if err != nil {
			return stats, err
		}
		if changed {
			stats.Changed++
			stats.Segments += n
		}
		if opt.Progress != nil {
			opt.Progress(i+1, len(files))
		}
	}
	return stats, st.SetMeta(ctx, rulesKey, rules)
}

func syncFile(ctx context.Context, st *store.Store, src source.Source, red *redact.Redactor, f source.File) (int, bool, error) {
	head, err := headHash(f.Path)
	if err != nil {
		return 0, false, err
	}
	cur := store.Cursor{Size: f.Size, MTime: f.MTime.UnixNano(), HeadHash: head}
	old, seen, err := st.Cursor(ctx, f.Path)
	if err != nil {
		return 0, false, err
	}
	if seen && old == cur {
		return 0, false, nil
	}

	var from int64
	var seq int
	if seen && f.Size >= old.Size && head == old.HeadHash {
		if off, s, ok, err := st.ResumePoint(ctx, f.Path); err != nil {
			return 0, false, err
		} else if ok {
			from, seq = off, s
		}
	}

	var recs []source.Record
	for r, err := range src.Parse(ctx, f, from) {
		if err != nil {
			return 0, false, err
		}
		// Titles and the first prompt are flattened into one line during segmentation, which
		// hides line-anchored secrets like .env assignments, so these are masked first.
		if r.Kind == source.Prompt || r.Kind == source.Title || r.Kind == source.AutoTitle {
			r.Text = red.Redact(r.Text)
		}
		recs = append(recs, r)
	}
	sess, segs := segment.Build(src.Name(), recs, seq)
	if sess.Title == "" && from == 0 {
		sess.Title = sess.FirstPrompt
	}
	for i := range segs {
		segs[i].Text = red.Redact(segs[i].Text)
		if len(segs[i].Atoms) > 0 {
			segs[i].Atoms = strings.Split(red.Redact(strings.Join(segs[i].Atoms, "\n")), "\n")
		}
	}

	err = st.WriteFile(ctx, store.FileUpdate{
		Path: f.Path, Source: src.Name(), Cursor: cur, FromSeq: seq, Session: sess, Segments: segs,
	})
	return len(segs), true, err
}

func headHash(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = fh.Close() }()
	line, err := bufio.NewReader(io.LimitReader(fh, 64<<10)).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	h := sha256.Sum256(line)
	return hex.EncodeToString(h[:]), nil
}

func rulesVersion(patterns []string) string {
	h := sha256.Sum256([]byte(strings.Join(patterns, "\x00")))
	return redact.Version + ":" + hex.EncodeToString(h[:8])
}
