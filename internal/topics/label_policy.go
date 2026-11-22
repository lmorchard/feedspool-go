package topics

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultSampleItems is the default number of cluster items shown to the labeler.
const DefaultSampleItems = 5

// DefaultGenericLabel is the last-resort replacement when a policy rule cannot
// supply one.
const DefaultGenericLabel = "Software Development"

const (
	policyMatchWord        = "word"
	policyMatchSubstring   = "substring"
	policyRequireAll       = "all"
	policyRequireMajority  = "majority"
	policyActionGeneralize = "generalize"
)

// DefaultLabelPolicyYAML is the embedded default policy.
//
//go:embed label_policy.yaml
var DefaultLabelPolicyYAML string

// LabelPolicy is a small, data-driven post-label correction layer. The
// engine is generic; the vocabularies and replacements live in YAML so new
// practical cases do not become new branches in Go.
type LabelPolicy struct {
	Rules []LabelRule `yaml:"rules"`
}

// LabelRule generalizes a label containing a term when the sampled items do
// not support it. Match is "word" (default) or "substring"; Require is "all"
// (default) or "majority"; Action is "generalize" (default).
type LabelRule struct {
	Name        string   `yaml:"name"`
	Terms       []string `yaml:"terms"`
	Match       string   `yaml:"match"`
	Require     string   `yaml:"require"`
	Action      string   `yaml:"action"`
	Replacement string   `yaml:"replacement"`
}

// PolicyMatch records what the policy found for one generated label.
type PolicyMatch struct {
	Label   string
	Rule    string
	Term    string
	Support int
	Total   int
	Changed bool
}

var wordBoundary = regexp.MustCompile(`[^a-z0-9+#]+`)

func containsWord(text, word string) bool {
	for _, token := range wordBoundary.Split(strings.ToLower(text), -1) {
		if token == word {
			return true
		}
	}
	return false
}

func containsTerm(text, term, match string) bool {
	if match == policyMatchSubstring {
		return strings.Contains(strings.ToLower(text), strings.ToLower(term))
	}
	return containsWord(text, strings.ToLower(term))
}

// LoadLabelPolicy reads a policy file, or the embedded default when path is
// empty. A policy is data: edit the file to add a category, vocabulary, or
// replacement.
func LoadLabelPolicy(path string) (*LabelPolicy, error) {
	if strings.TrimSpace(path) == "" {
		return parseLabelPolicy([]byte(DefaultLabelPolicyYAML))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read label policy %q: %w", path, err)
	}
	return parseLabelPolicy(data)
}

func parseLabelPolicy(data []byte) (*LabelPolicy, error) {
	var policy LabelPolicy
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("parse label policy: %w", err)
	}
	for i := range policy.Rules {
		rule := &policy.Rules[i]
		if strings.TrimSpace(rule.Name) == "" {
			rule.Name = fmt.Sprintf("rule-%d", i+1)
		}
		if len(rule.Terms) == 0 {
			return nil, fmt.Errorf("label policy rule %q has no terms", rule.Name)
		}
		for _, term := range rule.Terms {
			if strings.TrimSpace(term) == "" {
				return nil, fmt.Errorf("label policy rule %q has an empty term", rule.Name)
			}
		}
		if rule.Match == "" {
			rule.Match = policyMatchWord
		}
		if rule.Match != policyMatchWord && rule.Match != policyMatchSubstring {
			return nil, fmt.Errorf("label policy rule %q has unknown match %q", rule.Name, rule.Match)
		}
		if rule.Require == "" {
			rule.Require = policyRequireAll
		}
		if rule.Require != policyRequireAll && rule.Require != policyRequireMajority {
			return nil, fmt.Errorf("label policy rule %q has unknown require %q", rule.Name, rule.Require)
		}
		if rule.Action == "" {
			rule.Action = policyActionGeneralize
		}
		if rule.Action != policyActionGeneralize {
			return nil, fmt.Errorf("label policy rule %q has unknown action %q", rule.Name, rule.Action)
		}
	}
	return &policy, nil
}

// DefaultLabelPolicy returns the embedded policy, or an empty policy if the
// embedded YAML cannot be parsed (tests fail loudly on that separately).
func DefaultLabelPolicy() *LabelPolicy {
	policy, err := parseLabelPolicy([]byte(DefaultLabelPolicyYAML))
	if err != nil {
		return &LabelPolicy{}
	}
	return policy
}

// Evaluate returns the policy result for a generated label. It reports the
// first matching rule and term even when the label is supported, so callers
// can log both hits and no-ops.
func (p *LabelPolicy) Evaluate(label string, titles []string, generic string) PolicyMatch {
	match := PolicyMatch{Label: label, Total: len(titles)}
	if p == nil {
		return match
	}
	for _, rule := range p.Rules {
		for _, term := range rule.Terms {
			if !containsTerm(label, term, rule.Match) {
				continue
			}
			match.Rule = rule.Name
			match.Term = term
			for _, title := range titles {
				if containsTerm(title, term, rule.Match) {
					match.Support++
				}
			}
			required := match.Total
			if rule.Require == policyRequireMajority {
				required = match.Total/2 + 1
			}
			if match.Total > 0 && match.Support >= required {
				return match
			}
			match.Label = rule.Replacement
			if strings.TrimSpace(match.Label) == "" {
				match.Label = generic
			}
			if strings.TrimSpace(match.Label) == "" {
				match.Label = DefaultGenericLabel
			}
			match.Changed = true
			return match
		}
	}
	return match
}

// Apply returns the label, or the rule replacement when the label names a
// rule term that the sampled items do not sufficiently support.
func (p *LabelPolicy) Apply(label string, titles []string, generic string) (string, bool) {
	match := p.Evaluate(label, titles, generic)
	return match.Label, match.Changed
}

// GuardLabel is the compatibility shortcut: apply the embedded policy.
func GuardLabel(label string, titles []string, generic string) (string, bool) {
	return DefaultLabelPolicy().Apply(label, titles, generic)
}
