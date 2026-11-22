package search

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/yakovlef/tidemark/internal/store"
)

const (
	DefaultLimit = 8
	candidates   = 50
	perSession   = 2
	halfLifeDays = 120
	minDecay     = 0.2
	projectBoost = 1.5
)

type Query struct {
	Text    string
	Limit   int
	Source  string
	Project string
	Since   time.Time
	CWD     string
	Now     time.Time
}

type Hit struct {
	ID        string
	SessionID string
	Source    string
	Project   string
	Title     string
	Time      time.Time
	Snippet   string
	Score     float64
}

func Search(ctx context.Context, st *store.Store, q Query) ([]Hit, error) {
	terms := Terms(q.Text)
	if len(terms) == 0 {
		return nil, nil
	}
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	f := store.Filter{Source: q.Source, Project: q.Project}
	if !q.Since.IsZero() {
		f.Since = q.Since.Unix()
	}

	var hits []Hit
	seen := map[string]bool{}
	perSess := map[string]int{}
	for i, match := range []string{Match(terms, " "), Match(terms, " OR ")} {
		if i == 1 && (len(terms) == 1 || len(hits) >= q.Limit) {
			break
		}
		cands, err := st.Candidates(ctx, match, f, perSession, candidates)
		if err != nil {
			return nil, err
		}
		for _, h := range rank(cands, q) {
			if len(hits) == q.Limit {
				break
			}
			if seen[h.ID] || perSess[h.SessionID] == perSession {
				continue
			}
			seen[h.ID] = true
			perSess[h.SessionID]++
			hits = append(hits, h)
		}
	}
	return hits, nil
}

func rank(cands []store.Candidate, q Query) []Hit {
	hits := make([]Hit, 0, len(cands))
	for _, c := range cands {
		t := time.Unix(c.TS, 0)
		score := -c.BM25 * decay(q.Now.Sub(t))
		if sameProject(c.Project, q.CWD) {
			score *= projectBoost
		}
		hits = append(hits, Hit{
			ID: c.ID, SessionID: c.SessionID, Source: c.Source, Project: c.Project,
			Title: c.Title, Time: t, Snippet: c.Snippet, Score: score,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits
}

func decay(age time.Duration) float64 {
	days := max(age.Hours()/24, 0)
	return max(minDecay, math.Pow(0.5, days/halfLifeDays))
}

func sameProject(project, cwd string) bool {
	if project == "" || cwd == "" {
		return false
	}
	return cwd == project || strings.HasPrefix(cwd, strings.TrimSuffix(project, "/")+"/")
}

// Terms splits a query into words, dropping FTS5 syntax so user input can never break the MATCH.
func Terms(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
}

// Match quotes every term and makes the last one a prefix, so a half-typed word still matches.
func Match(terms []string, sep string) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = `"` + t + `"`
	}
	parts[len(parts)-1] += "*"
	return strings.Join(parts, sep)
}
