package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/yakovlef/tidemark/internal/config"
	"github.com/yakovlef/tidemark/internal/index"
	"github.com/yakovlef/tidemark/internal/source"
	"github.com/yakovlef/tidemark/internal/source/claudecode"
	"github.com/yakovlef/tidemark/internal/store"
)

var version = "dev"

const usage = `tidemark: searchable memory from the history your AI agents already wrote

Usage:
  tidemark <command> [flags]

Commands:
  sync      index new and changed sessions [--source NAME] [--reindex]
  version   print version
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "sync":
		return runSync(ctx, args[1:], stderr)
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func sources() []source.Source {
	return []source.Source{claudecode.New(claudecode.DefaultRoot())}
}

func runSync(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	only := fs.String("source", "", "sync only this source")
	reindex := fs.Bool("reindex", false, "drop the index and re-read every file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var srcs []source.Source
	for _, s := range sources() {
		if *only == "" || s.Name() == *only {
			srcs = append(srcs, s)
		}
	}
	if len(srcs) == 0 {
		fmt.Fprintf(stderr, "unknown source %q\n", *only)
		return 2
	}

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return fail(stderr, err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fail(stderr, err)
	}
	dbPath, err := store.DefaultPath()
	if err != nil {
		return fail(stderr, err)
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return fail(stderr, err)
	}
	defer func() { _ = st.Close() }()

	for _, src := range srcs {
		name := src.Name()
		stats, err := index.Sync(ctx, st, src, index.Options{
			Patterns: cfg.Redact.Patterns,
			Reindex:  *reindex,
			Progress: func(done, total int) { fmt.Fprintf(stderr, "\r%s: %d/%d files", name, done, total) },
		})
		if stats.Files > 0 {
			fmt.Fprintln(stderr)
		}
		if err != nil {
			return fail(stderr, fmt.Errorf("%s: %w", name, err))
		}
		note := ""
		if stats.Reindex {
			note = ", reindexed"
		}
		fmt.Fprintf(stderr, "%s: %d files, %d changed, %d segments%s\n", name, stats.Files, stats.Changed, stats.Segments, note)
	}
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "tidemark: %v\n", err)
	return 1
}
