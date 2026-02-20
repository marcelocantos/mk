package mk

import (
	"strings"
)

// Pattern represents a target or prerequisite pattern with named captures.
// e.g. "build/{config}/{name}.o" has captures ["config", "name"]
// and parts ["build/", "/", ".o"].
type Pattern struct {
	Parts    []string // literal parts between captures
	Captures []string // capture names
	Raw      string   // original pattern string
}

// ParsePattern parses a pattern string into a Pattern.
// Patterns use {name} for named captures.
func ParsePattern(s string) (Pattern, bool) {
	var parts []string
	var captures []string

	rest := s
	var current string
	hasCapture := false

	for len(rest) > 0 {
		idx := strings.IndexByte(rest, '{')
		if idx < 0 {
			current += rest
			break
		}
		end := strings.IndexByte(rest[idx:], '}')
		if end < 0 {
			current += rest
			break
		}
		end += idx

		hasCapture = true
		current += rest[:idx]
		parts = append(parts, current)
		current = ""
		captures = append(captures, rest[idx+1:end])
		rest = rest[end+1:]
	}
	parts = append(parts, current)

	if !hasCapture {
		return Pattern{Raw: s}, false
	}

	return Pattern{
		Parts:    parts,
		Captures: captures,
		Raw:      s,
	}, true
}

// Match attempts to match a concrete string against this pattern.
// Returns the captured values and true if it matches, nil and false otherwise.
func (p Pattern) Match(s string) (map[string]string, bool) {
	if len(p.Captures) == 0 {
		return nil, s == p.Raw
	}

	captures := make(map[string]string)
	return p.match(s, 0, captures)
}

func (p Pattern) match(s string, idx int, captures map[string]string) (map[string]string, bool) {
	// Must start with Parts[idx]
	prefix := p.Parts[idx]
	if !strings.HasPrefix(s, prefix) {
		return nil, false
	}
	s = s[len(prefix):]

	// If this is the last part, s must be empty
	if idx >= len(p.Captures) {
		if s == "" {
			return captures, true
		}
		return nil, false
	}

	// Try to match the capture: find the next literal part
	suffix := p.Parts[idx+1]
	captureName := p.Captures[idx]

	if idx+1 >= len(p.Parts)-1 && suffix == "" && idx+1 >= len(p.Captures) {
		// Last capture, no suffix after it — capture the rest
		if existing, ok := captures[captureName]; ok {
			if existing != s {
				return nil, false
			}
		} else {
			captures[captureName] = s
		}
		return captures, true
	}

	// Try each possible split point
	for i := 0; i <= len(s); i++ {
		candidate := s[:i]
		// Don't allow captures to contain /
		if strings.Contains(candidate, "/") {
			continue
		}

		if existing, ok := captures[captureName]; ok {
			if existing != candidate {
				continue
			}
		}

		capturesCopy := make(map[string]string)
		for k, v := range captures {
			capturesCopy[k] = v
		}
		capturesCopy[captureName] = candidate

		if result, ok := p.match(s[i:], idx+1, capturesCopy); ok {
			return result, true
		}
	}

	return nil, false
}

// Expand substitutes capture values into a pattern to produce a concrete string.
func (p Pattern) Expand(captures map[string]string) string {
	if len(p.Captures) == 0 {
		return p.Raw
	}

	var b strings.Builder
	for i, part := range p.Parts {
		b.WriteString(part)
		if i < len(p.Captures) {
			b.WriteString(captures[p.Captures[i]])
		}
	}
	return b.String()
}

// IsPattern returns true if this has any captures.
func (p Pattern) IsPattern() bool {
	return len(p.Captures) > 0
}
