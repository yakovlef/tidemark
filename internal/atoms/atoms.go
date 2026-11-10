package atoms

import (
	"regexp"
	"strings"
)

const (
	maxAtoms   = 50
	maxLineLen = 200
)

var (
	urlRe     = regexp.MustCompile(`https?://[^\s<>"'()\[\]]+`)
	errRe     = regexp.MustCompile(`(?i)\b(error|panic|traceback|fail(ed|ure)?|exit code)\b`)
	pathRe    = regexp.MustCompile(`(?:~|\.{1,2})?(?:/[\w.@-]+){2,}|\b[\w-]+(?:/[\w.-]+)*\.(?:go|py|ts|tsx|js|jsx|php|rb|rs|java|kt|sql|lua|sh|yaml|yml|json|toml|md|proto|tf)\b`)
	hashRe    = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	refRe     = regexp.MustCompile(`\b[\w.-]+/[\w.-]+#\d+\b|(?:^|\s)#\d{2,}\b|\b[A-Z][A-Z0-9]+-\d+\b`)
	digitRe   = regexp.MustCompile(`[0-9]`)
	letterRe  = regexp.MustCompile(`[a-f]`)
	commandRe = regexp.MustCompile(`(?m)^\$ Bash (.+)$`)
)

func Extract(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] || len(out) >= maxAtoms {
			return
		}
		seen[s] = true
		out = append(out, s)
	}

	for _, m := range commandRe.FindAllStringSubmatch(text, -1) {
		add(cut(m[1]))
	}
	for _, line := range strings.Split(text, "\n") {
		if errRe.MatchString(line) && !strings.HasPrefix(line, "$ ") {
			add(cut(line))
		}
	}
	for _, u := range urlRe.FindAllString(text, -1) {
		add(strings.TrimRight(u, ".,;:"))
	}

	rest := urlRe.ReplaceAllString(text, " ")
	for _, p := range pathRe.FindAllString(rest, -1) {
		add(p)
	}
	for _, h := range hashRe.FindAllString(rest, -1) {
		if digitRe.MatchString(h) && letterRe.MatchString(h) {
			add(h)
		}
	}
	for _, r := range refRe.FindAllString(rest, -1) {
		add(r)
	}
	return out
}

func cut(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLineLen {
		return s
	}
	r := []rune(s)
	if len(r) > maxLineLen {
		r = r[:maxLineLen]
	}
	return string(r)
}
