package mk

import (
	"testing"
)

func TestParsePattern(t *testing.T) {
	tests := []struct {
		input     string
		isPattern bool
		captures  []string
	}{
		{"foo.o", false, nil},
		{"build/{name}.o", true, []string{"name"}},
		{"build/{config}/{name}.o", true, []string{"config", "name"}},
		{"{a}/{b}/{c}", true, []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		p, ok := ParsePattern(tt.input)
		if ok != tt.isPattern {
			t.Errorf("ParsePattern(%q): isPattern = %v, want %v", tt.input, ok, tt.isPattern)
			continue
		}
		if ok {
			if len(p.Captures) != len(tt.captures) {
				t.Errorf("ParsePattern(%q): captures = %v, want %v", tt.input, p.Captures, tt.captures)
			}
			for i := range p.Captures {
				if i < len(tt.captures) && p.Captures[i] != tt.captures[i] {
					t.Errorf("ParsePattern(%q): capture[%d] = %q, want %q", tt.input, i, p.Captures[i], tt.captures[i])
				}
			}
		}
	}
}

func TestPatternMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		input    string
		match    bool
		captures map[string]string
	}{
		{"foo.o", "foo.o", true, nil},
		{"foo.o", "bar.o", false, nil},
		{"build/{name}.o", "build/foo.o", true, map[string]string{"name": "foo"}},
		{"build/{name}.o", "build/bar.o", true, map[string]string{"name": "bar"}},
		{"build/{name}.o", "build/.o", true, map[string]string{"name": ""}},
		{"build/{name}.o", "src/foo.o", false, nil},
		{"build/{config}/{name}.o", "build/debug/foo.o", true, map[string]string{"config": "debug", "name": "foo"}},
		{"build/{config}/{name}.o", "build/release/bar.o", true, map[string]string{"config": "release", "name": "bar"}},
		// Captures must not contain /
		{"build/{name}.o", "build/a/b.o", false, nil},
	}

	for _, tt := range tests {
		p, _ := ParsePattern(tt.pattern)
		caps, ok := p.Match(tt.input)
		if ok != tt.match {
			t.Errorf("Pattern(%q).Match(%q): match = %v, want %v", tt.pattern, tt.input, ok, tt.match)
			continue
		}
		if ok && tt.captures != nil {
			for k, v := range tt.captures {
				if caps[k] != v {
					t.Errorf("Pattern(%q).Match(%q): capture[%q] = %q, want %q", tt.pattern, tt.input, k, caps[k], v)
				}
			}
		}
	}
}

func TestPatternExpand(t *testing.T) {
	tests := []struct {
		pattern  string
		captures map[string]string
		want     string
	}{
		{"build/{name}.o", map[string]string{"name": "foo"}, "build/foo.o"},
		{"build/{config}/{name}.o", map[string]string{"config": "debug", "name": "bar"}, "build/debug/bar.o"},
	}

	for _, tt := range tests {
		p, _ := ParsePattern(tt.pattern)
		got := p.Expand(tt.captures)
		if got != tt.want {
			t.Errorf("Pattern(%q).Expand(%v) = %q, want %q", tt.pattern, tt.captures, got, tt.want)
		}
	}
}

func TestSameNamedCaptures(t *testing.T) {
	// Same capture name on both sides of a rule means values must match
	target, _ := ParsePattern("build/{name}.o")
	prereq, _ := ParsePattern("src/{name}.c")

	caps, ok := target.Match("build/foo.o")
	if !ok {
		t.Fatal("target should match")
	}

	expanded := prereq.Expand(caps)
	if expanded != "src/foo.c" {
		t.Errorf("prereq.Expand = %q, want %q", expanded, "src/foo.c")
	}
}
