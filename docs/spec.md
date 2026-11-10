# tidemark spec (v0.1 → v0.3)

Rationale for each choice is in [decisions.md](decisions.md).

## Problem

AI coding agents already write everything to disk. Claude Code keeps `~/.claude/projects/*/*.jsonl` and Codex keeps `~/.codex/sessions/YYYY/MM/DD/*.jsonl`, with prompts, answers, tool calls, errors and decisions. Nobody can search it, and every new session starts from zero.

Existing tools fall into two groups. Some need the agent to save memories explicitly (engram, basic-memory, mem0). Others depend on vectors and a heavy runtime (claude-mem, qmd).

## Goal

A single Go binary that indexes the history agents already wrote, without agent cooperation and retroactively. It makes that history searchable from any agent through MCP.

- BM25 only. No embeddings, no vector DB, no model downloads.
- Secrets are masked before anything is stored or returned.
- Works with no LLM. LLM-written session cards are an opt-in quality boost.

## Releases

| Version | Scope |
|---|---|
| v0.1.0 (~2026-11-14) | Claude Code source, segmentation, atoms, redaction, store, search, CLI (`init`, `sync`, `search`, `mcp`, `doctor`), MCP `search` + `get` with lazy sync |
| v0.2.0 (~2026-11-23) | Codex source, daemon (60 s polling) + `daemon install`, Russian stemming (if eval agrees), MCP `recent` (if needed), `init --install claude` |
| v0.3.0 (~2026-12-09..11) | session cards (none / Ollama / Anthropic Batch / OpenAI-compatible), eval with public corpus and baselines; public launch |

Later: Cursor, ChatGPT export, GitHub, Gemini CLI, Aider.

## Sources

```go
type Source interface {
    Name() string
    Files(ctx context.Context) ([]File, error)
    Parse(ctx context.Context, f File, from int64) iter.Seq2[Record, error]
}
```

**claudecode** reads `type=user|assistant` records.
- Fields: `message.content` is a string or blocks, plus `cwd`, `gitBranch`, `timestamp` and `sessionId`.
- The session title comes from the `custom-title` record. Fallback: first 80 chars of the first prompt.
- Skip: `thinking` blocks, system reminders, `isSidechain=true` records, `api-request*` and other bookkeeping records.

**codex** (v0.2) reads `response_item` records.
- Keep messages with role user/assistant, plus `function_call` and `function_call_output`.
- Skip `developer` messages and token events.

## Segmentation

A segment is one user prompt plus everything until the next user prompt.

Text by part:
- prompt and assistant text: in full
- `tool_use`: tool name and key args (command, file path, query, URL)
- `tool_result`: first 2 KB plus error-looking lines (`error`, `panic`, `Traceback`, `FAIL`, `exit code`)
- `tool_result` of our own tools (`mcp__tidemark__*`): dropped. Only the call stays.

Size cap is ~8 KB. Longer turns are split into parts `part=1..n`, and each part starts with the prompt.

Segment id is `sha256(source, session_id, seq, part)`. Upserts are idempotent by `content_hash`.

## Atoms

Atoms are extracted by code, not by an LLM. They are error strings, shell commands, file paths, git hashes, issue/PR refs and URLs. They are stored in their own FTS column so verbatim lookups hit.

## Redaction

Redaction runs on every segment before storage.

- gitleaks rules (library, `detect` package). Extra rules: `Authorization: Bearer …`, `.env`-style `KEY=value` where the key contains SECRET/TOKEN/PASSWORD/API_KEY, and DSN passwords (`scheme://user:pass@`).
- No entropy check, so hashes and UUIDs stay searchable.
- Each secret is replaced with `[REDACTED:<rule>]`.
- User regexes go in `redact.patterns` in the config (e.g. NDA client names). PII is untouched by default.
- Raw text is never stored. When rules change, `sync --reindex` re-reads the source files. The rules version is kept in `meta`.
- `internal/redact/testdata` holds a fixture corpus of secrets. CI fails if any of them survives.
- PR #6 measures binary size and speed. If they're bad, switch to embedded rules TOML with our own matcher.

## Storage

The database is SQLite via `modernc.org/sqlite` (pure Go, no CGO), in WAL mode with `busy_timeout`.

- Data: `~/.local/share/tidemark/tidemark.db` (`TIDEMARK_DATA_DIR`).
- Config: `~/.config/tidemark/config.toml`.
- Migrations: embedded SQL files with `PRAGMA user_version`.

```sql
CREATE TABLE meta     (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE files    (path TEXT PRIMARY KEY, source TEXT, size INT, mtime INT, offset INT, head_hash TEXT);
CREATE TABLE sessions (id TEXT PRIMARY KEY, source TEXT, project TEXT, branch TEXT, title TEXT, started_at INT, ended_at INT);
CREATE TABLE segments (id TEXT PRIMARY KEY, session_id TEXT, seq INT, part INT, ts INT, text TEXT, atoms TEXT,
                       path TEXT, byte_offset INT, content_hash TEXT);
CREATE INDEX segments_order ON segments(session_id, seq, part);
CREATE TABLE cards    (session_id TEXT PRIMARY KEY, title TEXT, summary TEXT, questions TEXT, keywords TEXT,
                       model TEXT, prompt_version INT, created_at INT);  -- v0.3
CREATE VIRTUAL TABLE fts USING fts5(title, keywords, questions, atoms, summary, text,
                       tokenize='porter unicode61 remove_diacritics 2');
```

- `fts` is a regular FTS5 table holding its own copy, with `rowid = segments.rowid`. Every change is a delete + insert.
- `title`, `summary`, `questions` and `keywords` come from the session (title) and the session card (v0.3), copied into each segment row.

**Incremental reads.** Each file has a cursor (size, mtime, byte offset, first-line hash). A file is re-read from the start if it shrinks or its first-line hash changes. The store commits after each file, so an interrupted sync resumes.

## Search

1. The query is tokenized, and each term is quoted. The last term gets a prefix `*`.
2. Run an AND query first. If it returns fewer than `limit` hits, OR results are appended below.
3. `bm25(fts, 4, 3, 3, 2, 2, 1)` weights the columns title, keywords, questions, atoms, summary, text. The top 50 are taken.
4. Rerank in Go: `score = -bm25 × max(0.2, 0.5^(age_days/120)) × (1.5 if same project as cwd)`.
5. At most 2 hits per session.
6. Filters: `source`, `project` (cwd prefix), `since`. The default limit is 8.

Golden tests run 20 queries on a synthetic corpus in CI. Target: p95 under 100 ms on 50k segments.

## MCP (stdio, `modelcontextprotocol/go-sdk`)

| Tool | Input | Output |
|---|---|---|
| `search` | query, limit?, source?, project?, since? | ≤8 hits: id, title, project, date, ~300-char snippet (~3 KB total) |
| `get` | id, neighbours? (default 1), cursor? | segment text + adjacent segments, ≤16 KB, cursor for the rest |

- The output is compact markdown text plus `structuredContent`.
- All tools are read-only.
- **Lazy sync:** if the last sync is older than 5 minutes, the server takes a file lock, runs an incremental sync, then answers. From v0.2 the daemon does this and MCP is read-only.
- Tool descriptions say when to call them: the user refers to past work, an error seen before, or a known repo. Test them by hand on Claude Code and Codex before each release.
- stdout carries only JSON-RPC. All logs go through `slog` to stderr. A CI test checks that stdout is clean after a handshake.

## CLI

Commands use stdlib `flag` with a subcommand switch.

- `tidemark init` creates the config, detects sources with file counts, runs the first sync, and prints connect commands (`claude mcp add tidemark -- tidemark mcp`, Codex `config.toml` block). It never writes other tools' configs.
- `tidemark sync [--source X] [--reindex]` shows progress on stderr.
- `tidemark search <q> [--json]`
- `tidemark mcp`
- `tidemark doctor` checks paths, DB, FTS5, last error per source and a redaction self-test.
- v0.2: `tidemark daemon` polls mtime every 60 s (no fsnotify), plus `daemon install` (launchd/systemd).
- v0.3: `tidemark cards run [--budget-usd X] [--dry-run] [--refresh]`

## Cards (v0.3)

Each session gets one card, made from its redacted, compacted text:

```json
{"title": "...", "summary": "2-3 sentences", "questions": ["how someone would ask for this later"], "keywords": ["synonyms, EN + RU forms"]}
```

- Providers: `none` (default), `ollama`, `anthropic` (Batch API for backfill), and `openai`-compatible.
- Backfill goes newest to oldest. `--dry-run` prints the session count and a cost estimate.
- Cards store `prompt_version` and are regenerated only with `--refresh`.

## Eval (v0.3)

- **Public:** a synthetic corpus in `eval/` (~200 sessions, 50 queries), reproducible by anyone.
- **Private:** own history. Only metrics and the method are published.
- **Protocol:** write the queries from memory first, label the expected sessions after, then freeze the set. Tune on one half and report the other half.
- **Baselines:** ripgrep over raw files, Ollama embeddings (`nomic-embed-text`), tidemark without cards, tidemark with cards.
- **Metrics:** recall@8, MRR, p50/p95 latency, index size, first-index time, card cost in USD.

## Layout

```
cmd/tidemark/
internal/source/claudecode/   internal/source/codex/ (v0.2)
internal/segment/             internal/atoms/
internal/redact/              internal/store/
internal/search/              internal/mcpserver/
internal/card/ (v0.3)
eval/ (v0.3)
docs/
```

## Process

- Trunk-based. Short branches, one PR per row below, squash-merged with a Conventional Commits title.
- A ruleset on main requires green CI from PR #1 on.
- release-please opens the release PRs. goreleaser publishes binaries and the `yakovlef/homebrew-tap` tap. The module also supports `go install github.com/yakovlef/tidemark/cmd/tidemark@latest`.
- Test fixtures are synthetic and reviewed by hand. Real sessions never go into the repo.

### v0.1.0 PRs

| # | Title |
|---|---|
| 1 | `chore: add go module, lint and CI` |
| 2 | `docs: add readme, spec and decisions` |
| 3 | `feat(store): add sqlite store with migrations` |
| 4 | `feat(source): parse claude code sessions` |
| 5 | `feat(segment): split sessions into segments with atoms` |
| 6 | `feat(redact): mask secrets before storing` |
| 7 | `feat(cli): add init and sync commands` |
| 8 | `feat(search): rank with bm25 and recency decay` |
| 9 | `feat(cli): add search command` |
| 10 | `feat(mcp): add stdio server with search and get` |
| 11 | `feat(cli): add doctor command` |
| 12 | `chore(release): add goreleaser and release-please` |

## Launch (v0.3)

- README: a vhs GIF of an agent finding a past error, "2 commands to working", the eval table, and a factual comparison with engram and claude-mem.
- CONTRIBUTING.md, issue templates and 3–5 good first issues (new sources through `Source`).
- Show HN, r/ClaudeAI, r/LocalLLaMA, r/golang, LinkedIn, Go Telegram channels. All texts in own voice, organic stars only.
