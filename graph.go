package mk

import (
	"fmt"
	"os"
	"strings"
)

// Graph represents the build dependency graph.
type Graph struct {
	rules    []resolvedRule
	patterns []patternRule
	vars     *Vars
	state    *BuildState
}

type resolvedRule struct {
	target  string
	prereqs []string
	recipe  []string
	isTask  bool
	stem    string // first capture value from pattern match
}

// WhyRebuild returns human-readable reasons why the target needs rebuilding,
// or nil if it is up to date.
func (g *Graph) WhyRebuild(target string) ([]string, error) {
	rule, err := g.Resolve(target)
	if err != nil {
		return nil, err
	}
	if len(rule.recipe) == 0 {
		return nil, nil
	}
	vars := g.vars.Clone()
	vars.Set("target", target)
	if len(rule.prereqs) > 0 {
		vars.Set("input", rule.prereqs[0])
	}
	vars.Set("inputs", strings.Join(rule.prereqs, " "))
	var lines []string
	for _, line := range rule.recipe {
		l := line
		for len(l) > 0 && (l[0] == '@' || l[0] == '-') {
			l = l[1:]
		}
		lines = append(lines, vars.Expand(l))
	}
	recipeText := strings.Join(lines, "\n")
	return g.state.WhyStale(target, rule.prereqs, recipeText), nil
}

type patternRule struct {
	targetPatterns []Pattern
	prereqPatterns []Pattern
	recipe         []string
}

// BuildGraph constructs a dependency graph from a parsed file.
func BuildGraph(file *File, vars *Vars, state *BuildState) (*Graph, error) {
	g := &Graph{
		vars:  vars,
		state: state,
	}

	if err := g.evaluate(file.Stmts); err != nil {
		return nil, err
	}

	return g, nil
}

func (g *Graph) evaluate(stmts []Node) error {
	for _, stmt := range stmts {
		if err := g.evalNode(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graph) evalNode(node Node) error {
	switch n := node.(type) {
	case VarAssign:
		value := n.Value
		if !n.Lazy {
			value = g.vars.Expand(value)
		}
		switch n.Op {
		case OpSet:
			if n.Lazy {
				g.vars.SetLazy(n.Name, n.Value)
			} else {
				g.vars.Set(n.Name, value)
			}
		case OpAppend:
			g.vars.Append(n.Name, g.vars.Expand(n.Value))
		case OpCondSet:
			if g.vars.Get(n.Name) == "" {
				g.vars.Set(n.Name, value)
			}
		}

	case Rule:
		return g.addRule(n)

	case Conditional:
		return g.evalConditional(n)

	case Include:
		// TODO: implement include
		return fmt.Errorf("include not yet implemented")
	}

	return nil
}

func (g *Graph) addRule(r Rule) error {
	// Expand variable references in targets and prereqs
	var expandedTargets []string
	for _, t := range r.Targets {
		expandedTargets = append(expandedTargets, g.vars.Expand(t))
	}

	var expandedPrereqs []string
	for _, p := range r.Prereqs {
		expanded := g.vars.Expand(p)
		// Handle substitution references: $var:old=new
		expandedPrereqs = append(expandedPrereqs, strings.Fields(expanded)...)
	}

	// Check if any target is a pattern
	isPattern := false
	for _, t := range expandedTargets {
		if _, ok := ParsePattern(t); ok {
			isPattern = true
			break
		}
	}

	if isPattern {
		pr := patternRule{recipe: r.Recipe}
		for _, t := range expandedTargets {
			p, _ := ParsePattern(t)
			pr.targetPatterns = append(pr.targetPatterns, p)
		}
		for _, p := range expandedPrereqs {
			pat, _ := ParsePattern(p)
			pr.prereqPatterns = append(pr.prereqPatterns, pat)
		}
		g.patterns = append(g.patterns, pr)
	} else {
		// Explicit rule — may expand to multiple targets
		for _, t := range expandedTargets {
			g.rules = append(g.rules, resolvedRule{
				target:  t,
				prereqs: expandedPrereqs,
				recipe:  r.Recipe,
				isTask:  r.IsTask,
			})
		}
	}

	return nil
}

func (g *Graph) evalConditional(c Conditional) error {
	for _, branch := range c.Branches {
		if branch.Op == "else" {
			return g.evaluate(branch.Body)
		}
		left := g.vars.Expand(branch.Left)
		right := g.vars.Expand(branch.Right)

		match := false
		switch branch.Cmp {
		case "==":
			match = left == right
		case "!=":
			match = left != right
		}
		if match {
			return g.evaluate(branch.Body)
		}
	}
	return nil
}

// Resolve finds the rule for a given target, including pattern matching.
func (g *Graph) Resolve(target string) (*resolvedRule, error) {
	// Check explicit rules first
	for i := range g.rules {
		if g.rules[i].target == target {
			return &g.rules[i], nil
		}
	}

	// Try pattern rules
	for _, pr := range g.patterns {
		for _, tp := range pr.targetPatterns {
			captures, ok := tp.Match(target)
			if !ok {
				continue
			}

			// Expand prerequisite patterns with captures
			var prereqs []string
			for _, pp := range pr.prereqPatterns {
				prereqs = append(prereqs, pp.Expand(captures))
			}

			// Expand captures in recipe
			var recipe []string
			for _, line := range pr.recipe {
				expanded := line
				for k, v := range captures {
					expanded = strings.ReplaceAll(expanded, "{"+k+"}", v)
				}
				recipe = append(recipe, expanded)
			}

			// Use the first capture value as stem
			var stem string
			if len(tp.Captures) > 0 {
				stem = captures[tp.Captures[0]]
			}

			r := &resolvedRule{
				target:  target,
				prereqs: prereqs,
				recipe:  recipe,
				stem:    stem,
			}
			return r, nil
		}
	}

	// Check if the target exists as a file (leaf node)
	if fileExists(target) {
		return &resolvedRule{target: target}, nil
	}

	return nil, fmt.Errorf("no rule to build %q", target)
}

// DefaultTarget returns the first explicit non-task target.
func (g *Graph) DefaultTarget() string {
	for _, r := range g.rules {
		if !r.isTask {
			return r.target
		}
	}
	if len(g.rules) > 0 {
		return g.rules[0].target
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
