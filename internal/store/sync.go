package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/yakovlef/tidemark/internal/segment"
)

type Cursor struct {
	Size     int64
	MTime    int64
	HeadHash string
}

type FileUpdate struct {
	Path     string
	Source   string
	Cursor   Cursor
	FromSeq  int
	Session  segment.Session
	Segments []segment.Segment
}

func (s *Store) Cursor(ctx context.Context, path string) (Cursor, bool, error) {
	var c Cursor
	err := s.db.QueryRowContext(ctx, "SELECT size, mtime, head_hash FROM files WHERE path = ?", path).
		Scan(&c.Size, &c.MTime, &c.HeadHash)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// ResumePoint returns where the last stored turn of a file starts, so an appended file is
// re-read from that turn and a turn that was still being written gets rebuilt.
func (s *Store) ResumePoint(ctx context.Context, path string) (offset int64, seq int, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		"SELECT byte_offset, seq FROM segments WHERE path = ? ORDER BY seq DESC, part DESC LIMIT 1", path).
		Scan(&offset, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	return offset, seq, err == nil, err
}

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

func (s *Store) Reset(ctx context.Context, source string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		"DELETE FROM fts WHERE rowid IN (SELECT segments.rowid FROM segments JOIN sessions ON sessions.id = segments.session_id WHERE sessions.source = ?)",
		"DELETE FROM sessions WHERE source = ?",
		"DELETE FROM files WHERE source = ?",
	} {
		if _, err := tx.ExecContext(ctx, q, source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// WriteFile replaces the segments of one file from FromSeq on and moves its cursor, in one
// transaction, so an interrupted sync resumes at the first file that was not committed.
func (s *Store) WriteFile(ctx context.Context, u FileUpdate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		"DELETE FROM fts WHERE rowid IN (SELECT rowid FROM segments WHERE path = ? AND seq >= ?)", u.Path, u.FromSeq); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM segments WHERE path = ? AND seq >= ?", u.Path, u.FromSeq); err != nil {
		return err
	}

	if sess := u.Session; sess.ID != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sessions (id, source, project, branch, title, started_at, ended_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				project    = CASE WHEN excluded.project <> '' THEN excluded.project ELSE project END,
				branch     = CASE WHEN excluded.branch <> '' THEN excluded.branch ELSE branch END,
				title      = CASE WHEN excluded.title <> '' THEN excluded.title ELSE title END,
				started_at = CASE WHEN started_at = 0 OR (excluded.started_at > 0 AND excluded.started_at < started_at)
				                  THEN excluded.started_at ELSE started_at END,
				ended_at   = MAX(ended_at, excluded.ended_at)`,
			sess.ID, u.Source, sess.Project, sess.Branch, sess.Title, unix(sess.Start.Unix()), unix(sess.End.Unix())); err != nil {
			return err
		}

		for _, seg := range u.Segments {
			atoms := strings.Join(seg.Atoms, "\n")
			var rowid int64
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO segments (id, session_id, seq, part, ts, text, atoms, path, byte_offset, content_hash)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(id) DO UPDATE SET
					seq = excluded.seq, part = excluded.part, ts = excluded.ts, text = excluded.text,
					atoms = excluded.atoms, path = excluded.path, byte_offset = excluded.byte_offset,
					content_hash = excluded.content_hash
				RETURNING rowid`,
				seg.ID, seg.SessionID, seg.Seq, seg.Part, unix(seg.Time.Unix()), seg.Text, atoms,
				u.Path, seg.Offset, seg.Hash).Scan(&rowid); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM fts WHERE rowid = ?", rowid); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO fts (rowid, title, keywords, questions, atoms, summary, text) VALUES (?, '', '', '', ?, '', ?)",
				rowid, atoms, seg.Text); err != nil {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE fts SET title = (SELECT title FROM sessions WHERE id = ?)
			WHERE rowid IN (SELECT rowid FROM segments WHERE session_id = ?)`, sess.ID, sess.ID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO files (path, source, size, mtime, offset, head_hash) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			source = excluded.source, size = excluded.size, mtime = excluded.mtime,
			offset = excluded.offset, head_hash = excluded.head_hash`,
		u.Path, u.Source, u.Cursor.Size, u.Cursor.MTime, u.Cursor.Size, u.Cursor.HeadHash); err != nil {
		return err
	}
	return tx.Commit()
}

func unix(sec int64) int64 {
	if sec < 0 {
		return 0
	}
	return sec
}
