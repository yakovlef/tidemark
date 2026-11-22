package store

import (
	"context"
	"strings"
)

type Filter struct {
	Source  string
	Project string
	Since   int64
}

type Candidate struct {
	ID        string
	SessionID string
	Source    string
	Project   string
	Title     string
	TS        int64
	Snippet   string
	BM25      float64
}

// Candidates returns up to limit segments matching an FTS5 expression, best bm25 first,
// taking at most perSession from each session so one long session cannot fill the list.
func (s *Store) Candidates(ctx context.Context, match string, f Filter, perSession, limit int) ([]Candidate, error) {
	q := `
		SELECT fts.rowid, seg.id, seg.session_id, ses.source, ses.project, ses.title, seg.ts,
		       bm25(fts, 4, 3, 3, 2, 2, 1) AS score
		FROM fts
		JOIN segments seg ON seg.rowid = fts.rowid
		JOIN sessions ses ON ses.id = seg.session_id
		WHERE fts MATCH ?`
	args := []any{match}
	if f.Source != "" {
		q += " AND ses.source = ?"
		args = append(args, f.Source)
	}
	if f.Project != "" {
		q += " AND (ses.project = ? OR ses.project LIKE ? ESCAPE '\\')"
		args = append(args, f.Project, escapeLike(strings.TrimSuffix(f.Project, "/"))+"/%")
	}
	if f.Since > 0 {
		q += " AND seg.ts >= ?"
		args = append(args, f.Since)
	}
	q = `
		SELECT c.id, c.session_id, c.source, c.project, c.title, c.ts, snippet(fts, -1, '', '', '…', 48), c.score
		FROM (SELECT * FROM (SELECT *, row_number() OVER (PARTITION BY session_id ORDER BY score) AS n FROM (` + q + `))
		      WHERE n <= ? ORDER BY score LIMIT ?) c
		JOIN fts ON fts.rowid = c.rowid
		WHERE fts MATCH ?
		ORDER BY c.score`
	args = append(args, perSession, limit, match)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.ID, &c.SessionID, &c.Source, &c.Project, &c.Title, &c.TS, &c.Snippet, &c.BM25); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
