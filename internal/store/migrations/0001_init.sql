CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE files (
	path      TEXT PRIMARY KEY,
	source    TEXT NOT NULL,
	size      INTEGER NOT NULL,
	mtime     INTEGER NOT NULL,
	offset    INTEGER NOT NULL,
	head_hash TEXT NOT NULL
);

CREATE TABLE sessions (
	id         TEXT PRIMARY KEY,
	source     TEXT NOT NULL,
	project    TEXT NOT NULL DEFAULT '',
	branch     TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL,
	ended_at   INTEGER NOT NULL
);

CREATE TABLE segments (
	id           TEXT PRIMARY KEY,
	session_id   TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	seq          INTEGER NOT NULL,
	part         INTEGER NOT NULL DEFAULT 1,
	ts           INTEGER NOT NULL,
	text         TEXT NOT NULL,
	atoms        TEXT NOT NULL DEFAULT '',
	path         TEXT NOT NULL,
	byte_offset  INTEGER NOT NULL,
	content_hash TEXT NOT NULL
);

CREATE INDEX segments_order ON segments(session_id, seq, part);

CREATE VIRTUAL TABLE fts USING fts5(
	title, keywords, questions, atoms, summary, text,
	tokenize = 'porter unicode61 remove_diacritics 2'
);
