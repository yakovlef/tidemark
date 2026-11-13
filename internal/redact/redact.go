package redact

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zricethezav/gitleaks/v8/detect"
)

const Version = "2"

type rule struct {
	name string
	re   *regexp.Regexp
}

var builtin = []rule{
	{"bearer", regexp.MustCompile(`(?i)\bauthorization:\s*bearer\s+([^\s"'\x60$<]+)`)},
	{"env", regexp.MustCompile(`(?m)^\s*(?:export\s+)?[A-Z0-9_]*(?:SECRET|TOKEN|PASSWORD|PASSWD|API_KEY|APIKEY)[A-Z0-9_]*\s*[=:]\s*["']?([^\s"'#$<]+)`)},
	{"anthropic", regexp.MustCompile(`\b(sk-ant-[a-z]+\d*-[A-Za-z0-9_-]{20,})`)},
	{"dsn", regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:([^\s@/$<]+)@`)},
}

// generic-api-key is entropy based and hides hashes and addresses that should stay searchable.
var skipGitleaks = map[string]bool{"generic-api-key": true}

type Redactor struct {
	gitleaks *detect.Detector
	rules    []rule
}

func New(patterns []string) (*Redactor, error) {
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, err
	}
	rules := append([]rule{}, builtin...)
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("redact pattern %q: %w", p, err)
		}
		rules = append(rules, rule{"custom", re})
	}
	return &Redactor{gitleaks: d, rules: rules}, nil
}

type span struct {
	start, end int
	rule       string
}

func (r *Redactor) Redact(text string) string {
	var spans []span
	for _, f := range r.gitleaks.DetectString(text) {
		if skipGitleaks[f.RuleID] || f.Secret == "" {
			continue
		}
		in := strings.Index(f.Match, f.Secret)
		if in < 0 {
			continue
		}
		for off := 0; ; {
			i := strings.Index(text[off:], f.Match)
			if i < 0 {
				break
			}
			s := off + i + in
			spans = append(spans, span{s, s + len(f.Secret), f.RuleID})
			off += i + len(f.Match)
		}
	}
	for _, ru := range r.rules {
		for _, m := range ru.re.FindAllStringSubmatchIndex(text, -1) {
			if len(m) >= 4 && m[2] >= 0 {
				spans = append(spans, span{m[2], m[3], ru.name})
			} else {
				spans = append(spans, span{m[0], m[1], ru.name})
			}
		}
	}
	if len(spans) == 0 {
		return text
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	var b strings.Builder
	pos := 0
	for _, s := range spans {
		if s.start < pos {
			continue
		}
		b.WriteString(text[pos:s.start])
		b.WriteString("[REDACTED:" + s.rule + "]")
		pos = s.end
	}
	b.WriteString(text[pos:])
	return b.String()
}
