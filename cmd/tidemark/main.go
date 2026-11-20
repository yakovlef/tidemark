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
  init      create the config, find sources and run the first sync
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
	case "init":
		return runInit(ctx, args[1:], stdout, stderr)
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

	return syncAll(ctx, srcs, *reindex, stderr)
}

func syncAll(ctx context.Context, srcs []source.Source, reindex bool, stderr io.Writer) int {
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
			Reindex:  reindex,
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

const connect = `
Connect an agent:

  Claude Code:  claude mcp add tidemark -- tidemark mcp

  Codex (~/.codex/config.toml):
    [mcp_servers.tidemark]
    command = "tidemark"
    args = ["mcp"]
`

func runInit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfgPath, err := config.DefaultPath()
	if err != nil {
		return fail(stderr, err)
	}
	created, err := config.Init(cfgPath)
	if err != nil {
		return fail(stderr, err)
	}
	if created {
		fmt.Fprintf(stdout, "created %s\n", cfgPath)
	} else {
		fmt.Fprintf(stdout, "config %s already exists, left as is\n", cfgPath)
	}

	var found []source.Source
	for _, src := range sources() {
		files, err := src.Files(ctx)
		if err != nil {
			return fail(stderr, fmt.Errorf("%s: %w", src.Name(), err))
		}
		fmt.Fprintf(stdout, "%s: %d files\n", src.Name(), len(files))
		if len(files) > 0 {
			found = append(found, src)
		}
	}
	if len(found) == 0 {
		fmt.Fprintln(stdout, "no agent history found, nothing to index yet")
	} else if code := syncAll(ctx, found, false, stderr); code != 0 {
		return code
	}

	fmt.Fprint(stdout, connect)
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "tidemark: %v\n", err)
	return 1
}
