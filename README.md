# tidemark

Searchable memory from the history your AI agents already wrote.

Claude Code, Codex and other agents keep every session on disk: prompts, answers, commands, errors, decisions. tidemark indexes that history locally (SQLite FTS5, BM25, no vectors), masks secrets, and lets any agent search it over MCP.

> Work in progress. First release (v0.1.0) is planned for mid November 2026.

- [Spec](docs/spec.md)
- [Design decisions](docs/decisions.md)

## License

MIT
