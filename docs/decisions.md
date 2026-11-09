# Decisions

## 2026-11-08 Process
- Trunk-based. Every change goes through a short branch and a PR, CI must be green, and the PR is squash-merged with a Conventional Commits title. main always builds.
- SemVer, staying on 0.x until CLI, MCP and DB format are stable.
- Releases are sliced: v0.1.0 has Claude Code, redaction, search and MCP. v0.2.0 adds Codex and the daemon. v0.3.0 adds cards and eval. The public launch happens at v0.3.
- release-please maintains CHANGELOG and release PRs. goreleaser builds binaries and the brew tap from the tag.

## 2026-11-08 v0.1.0 plan
- 12 PRs in this order: CI → docs → store → claudecode source → segment → redact → sync → search → search CLI → MCP → doctor → release tooling. Tag v0.1.0 around 2026-11-14 (moved up, see below).
- Test fixtures are synthetic: generated from the real field structure and reviewed by hand. Real sessions never go into the repo.
- Once CI exists (after PR #1), a ruleset on main allows merges only through PRs with green CI.
- Redaction is mandatory in v0.1, because MCP results end up in cloud LLM prompts.

## 2026-11-08 Sources and segmentation
- Segment = user prompt + everything until the next prompt, capped at ~8 KB. Longer turns are split into parts seq.1, seq.2…, and each part repeats the prompt as its header.
- Segment text: prompt and assistant text in full; tool_use = tool name + key args (command, path, query); tool_result = first 2 KB + error lines. Skip thinking and system reminders.
- Session title comes from the Claude Code `custom-title` record. Fallback: first 80 chars of the first prompt.
- Sidechain (subagent) records are skipped in v0.1.

## 2026-11-08 Redaction
- Rules only, no entropy check: gitleaks rules plus our own (Authorization Bearer, .env lines whose key names contain SECRET/TOKEN/PASSWORD/API_KEY, passwords in DSNs). Hashes and UUIDs stay searchable.
- gitleaks is used as a library (detect package). Measure binary size and speed in PR #6. If they're bad, switch to embedded rules TOML with our own matcher.
- Raw text is never stored. When rules change, `sync --reindex` re-reads the source files.
- PII is left as is by default. `redact.patterns` in the config holds the user's own regexes (e.g. NDA client names).

## 2026-11-09 Storage
- Index freshness in v0.1: the MCP server runs an incremental sync when the last one is older than 5 minutes. It takes a file lock first, and SQLite runs in WAL mode with busy_timeout. From v0.2 the daemon does this job and MCP is read-only.
- FTS5 is a regular table holding its own copy, with rowid = segments.rowid. Changes are applied as delete + insert. No external content.
- Migrations are embedded SQL files with PRAGMA user_version. No framework.
- Schema adds segments.part, segments.path, segments.byte_offset, an index on (session_id, seq, part), and a meta(key, value) table (redaction rules version, last sync per source).
- Data dir is ~/.local/share/tidemark, overridable with TIDEMARK_DATA_DIR. Config is ~/.config/tidemark/config.toml.

## 2026-11-09 Cards
- Atoms (errors, commands, paths, hashes, issue refs) are extracted by code already in v0.1. They go in their own FTS column with weight 2.
- The LLM writes one card per session: title, summary, search questions, EN+RU synonyms. Segments inherit the card's keywords.
- Default provider is none. The README shows Ollama first and cloud second. Backfill goes newest to oldest with --budget-usd and --dry-run (prints count and cost estimate). Anthropic uses the Batch API.
- Cards store prompt_version. They are regenerated only with `cards run --refresh`.

## 2026-11-09 Search
- Run an AND query first. If it returns fewer than `limit` hits, append OR results below.
- Tokenizer in v0.1 is `porter unicode61 remove_diacritics 2`, plus a prefix match on the last term. A Russian Snowball stemmer comes in v0.2 as a separate stemmed column, after measuring it in eval.
- Hits from the current project (MCP process cwd) get ×1.5. `search` also takes a `project` param. At most 2 hits per session.
- 20 golden queries on the synthetic corpus run in CI starting with PR #8.

## 2026-11-09 MCP
- When indexing, drop tool_result of our own tools (mcp__tidemark__*) and keep only the call itself.
- v0.1 tools are `search` and `get` only. `recent` comes in v0.2 if real use needs it. `sources` is replaced by `doctor`.
- `search` returns ~3 KB (8 hits, ~300-char snippets). `get` returns at most 16 KB, continued via a cursor. Output is markdown text plus structuredContent.
- Tool descriptions say when to call them (past work, a known repo). Test them by hand on Claude Code and Codex before the release.

## 2026-11-09 CLI and daemon
- In MCP mode stdout carries only the protocol. All logs go through slog to stderr. A CI test runs a handshake and checks that stdout is clean.
- `init` creates the config, detects sources (file counts), runs the first sync and prints connect commands for Claude Code and Codex. It does not write other tools' configs. `--install claude` comes in v0.2.
- No fsnotify. Every 60 s, compare file mtime and size. The same scan backs the MCP lazy sync in v0.1.
- `sync` shows progress on stderr and commits after each file, so an interrupted run resumes.
- Stdlib `flag` with a subcommand switch, no cobra. `search` has a `--json` flag.

## 2026-11-09 Eval
- Two datasets. Public: a synthetic corpus in the repo (~200 sessions, 50 queries), reproducible by anyone. Private: own history; only metrics and the method are published.
- Protocol: write 50 queries from memory first, label the expected session after, then freeze the set. Tune weights on one half and report the other half.
- Baselines: ripgrep over raw files, Ollama embeddings (nomic-embed-text), tidemark without cards, tidemark with cards.
- Metrics: recall@8, MRR, p50/p95 latency, index size, first-index time, card cost in USD.

## 2026-11-09 Distribution and launch
- Own tap yakovlef/homebrew-tap via goreleaser, plus `go install …/cmd/tidemark@latest`. No Apple signing. Check how macOS treats the unsigned binary in PR #12.
- README: a vhs terminal GIF at the top, "2 commands to working", the eval table, and a factual comparison with engram and claude-mem.
- Launch at v0.3: Show HN (Tue–Thu, US morning), r/ClaudeAI, r/LocalLLaMA, r/golang, LinkedIn, Russian Go Telegram channels. A follow-up article about the benchmark. All texts in own voice, organic stars only.
- By launch: CONTRIBUTING.md, issue templates, 3–5 good first issues (e.g. new sources through the Source interface).
- Weekly archive of GitHub traffic (views, clones, referrers), plus a monthly Insights screenshot, for the visa case.

## 2026-11-09 Dates moved up
- v0.1.0 ~2026-11-14, v0.2.0 ~2026-11-23, v0.3.0 + launch ~2026-12-09..11. Launching before the holidays matters because HN and Reddit traffic drops in late December. If we miss it, the fallback is 2027-01-12..15.
- Pace is 1–3 PRs a day. No merging everything in one burst.
- Before the launch, use tidemark daily on own sessions for at least a week. This is where ranking and MCP descriptions get tuned.
