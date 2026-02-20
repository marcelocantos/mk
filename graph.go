package mk

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Graph represents the build dependency graph.
type Graph struct {
	rules       []resolvedRule
	patterns    []patternRule
	vars        *Vars
	state       *BuildState
	scopePrefix string // current include scope path prefix (e.g., "lib/")
}

type resolvedRule struct {
	target           string   // first listed target (for $target)
	targets          []string // all output targets (for multi-output rules)
	prereqs          []string
	orderOnlyPrereqs []string
	recipe           []string
	isTask           bool
	keep             bool   // [keep] annotation — don't delete on error
	fingerprint      string // [fingerprint: command] for non-file artifacts
	stem             string // first capture value from pattern match
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
	vars.Set("target", rule.target)
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
	fingerprint := rule.fingerprint
	if fingerprint != "" {
		fingerprint = vars.Expand(fingerprint)
	}
	return g.state.WhyStale(rule.targets, rule.prereqs, recipeText, fingerprint), nil
}

type patternRule struct {
	targetPatterns         []Pattern
	prereqPatterns         []Pattern
	orderOnlyPrereqPatterns []Pattern
	recipe                 []string
	keep                   bool
	fingerprint            string
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
		return g.evalInclude(n)
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
		expandedPrereqs = append(expandedPrereqs, strings.Fields(expanded)...)
	}

	var expandedOrderOnly []string
	for _, p := range r.OrderOnlyPrereqs {
		expanded := g.vars.Expand(p)
		expandedOrderOnly = append(expandedOrderOnly, strings.Fields(expanded)...)
	}

	// Rebase paths under scope prefix
	if g.scopePrefix != "" {
		for i, t := range expandedTargets {
			expandedTargets[i] = filepath.Clean(filepath.Join(g.scopePrefix, t))
		}
		for i, p := range expandedPrereqs {
			expandedPrereqs[i] = filepath.Clean(filepath.Join(g.scopePrefix, p))
		}
		for i, p := range expandedOrderOnly {
			expandedOrderOnly[i] = filepath.Clean(filepath.Join(g.scopePrefix, p))
		}
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
		pr := patternRule{recipe: r.Recipe, keep: r.Keep, fingerprint: r.Fingerprint}
		for _, t := range expandedTargets {
			p, _ := ParsePattern(t)
			pr.targetPatterns = append(pr.targetPatterns, p)
		}
		for _, p := range expandedPrereqs {
			pat, _ := ParsePattern(p)
			pr.prereqPatterns = append(pr.prereqPatterns, pat)
		}
		for _, p := range expandedOrderOnly {
			pat, _ := ParsePattern(p)
			pr.orderOnlyPrereqPatterns = append(pr.orderOnlyPrereqPatterns, pat)
		}
		g.patterns = append(g.patterns, pr)
	} else {
		// Explicit rule — one resolvedRule with all targets grouped
		g.rules = append(g.rules, resolvedRule{
			target:           expandedTargets[0],
			targets:          expandedTargets,
			prereqs:          expandedPrereqs,
			orderOnlyPrereqs: expandedOrderOnly,
			recipe:           r.Recipe,
			isTask:           r.IsTask,
			keep:             r.Keep,
			fingerprint:      r.Fingerprint,
		})
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

func (g *Graph) evalInclude(inc Include) error {
	path := g.vars.Expand(inc.Path)

	// Pattern discovery: include {path}/mkfile as {path}
	if strings.Contains(path, "{") {
		return g.evalPatternInclude(path, inc.Alias)
	}

	// Resolve path relative to current scope
	if g.scopePrefix != "" {
		path = filepath.Join(g.scopePrefix, path)
	}

	return g.doInclude(path, inc.Alias)
}

func (g *Graph) evalPatternInclude(pattern, _ string) error {
	// Replace {name} with * for globbing
	globPattern := pattern
	for {
		start := strings.IndexByte(globPattern, '{')
		if start < 0 {
			break
		}
		end := strings.IndexByte(globPattern[start:], '}')
		if end < 0 {
			break
		}
		globPattern = globPattern[:start] + "*" + globPattern[start+end+1:]
	}

	if g.scopePrefix != "" {
		globPattern = filepath.Join(g.scopePrefix, globPattern)
	}

	matches, err := filepath.Glob(globPattern)
	if err != nil {
		return fmt.Errorf("include glob %q: %w", globPattern, err)
	}

	for _, match := range matches {
		dir := filepath.Dir(match)
		// Strip scopePrefix to get the alias
		alias := dir
		if g.scopePrefix != "" {
			alias, _ = filepath.Rel(g.scopePrefix, dir)
		}
		if err := g.doInclude(match, alias); err != nil {
			return err
		}
	}
	return nil
}

func (g *Graph) doInclude(path, alias string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", path, err)
	}
	defer f.Close()

	ast, err := Parse(f)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	if alias == "" {
		// Unscoped include — paste directly into current scope
		return g.evaluate(ast.Stmts)
	}

	// Scoped include — isolate variables, rebase paths
	parentVars := g.vars
	parentPrefix := g.scopePrefix

	childVars := parentVars.Clone()
	parentSnapshot := parentVars.Snapshot()

	g.vars = childVars
	g.scopePrefix = filepath.Dir(path)
	if g.scopePrefix == "." {
		g.scopePrefix = alias
	}

	err = g.evaluate(ast.Stmts)

	// Export child-set variables as alias.varname to parent
	childSnapshot := childVars.Snapshot()
	for k, v := range childSnapshot {
		if old, exists := parentSnapshot[k]; !exists || old != v {
			parentVars.Set(alias+"."+k, v)
		}
	}

	// Restore parent scope
	g.vars = parentVars
	g.scopePrefix = parentPrefix

	return err
}

// Resolve finds the rule for a given target, including pattern matching.
func (g *Graph) Resolve(target string) (*resolvedRule, error) {
	// Check explicit rules first (match against any target in the group)
	for i := range g.rules {
		for _, t := range g.rules[i].targets {
			if t == target {
				return &g.rules[i], nil
			}
		}
	}

	// Try pattern rules
	for _, pr := range g.patterns {
		for _, tp := range pr.targetPatterns {
			captures, ok := tp.Match(target)
			if !ok {
				continue
			}

			// Expand ALL target patterns with captures
			var targets []string
			for _, tp2 := range pr.targetPatterns {
				targets = append(targets, tp2.Expand(captures))
			}

			// Expand prerequisite patterns with captures
			var prereqs []string
			for _, pp := range pr.prereqPatterns {
				prereqs = append(prereqs, pp.Expand(captures))
			}

			// Expand order-only prerequisite patterns with captures
			var orderOnly []string
			for _, pp := range pr.orderOnlyPrereqPatterns {
				orderOnly = append(orderOnly, pp.Expand(captures))
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

			// Expand captures in fingerprint command
			fp := pr.fingerprint
			for k, v := range captures {
				fp = strings.ReplaceAll(fp, "{"+k+"}", v)
			}

			r := &resolvedRule{
				target:           targets[0],
				targets:          targets,
				prereqs:          prereqs,
				orderOnlyPrereqs: orderOnly,
				recipe:           recipe,
				keep:             pr.keep,
				fingerprint:      fp,
				stem:             stem,
			}
			return r, nil
		}
	}

	// Check if the target exists as a file (leaf node)
	if fileExists(target) {
		return &resolvedRule{target: target, targets: []string{target}}, nil
	}

	return nil, fmt.Errorf("no rule to build %q", target)
}

// PrintGraph prints the dependency subgraph rooted at the given targets as DOT.
func (g *Graph) PrintGraph(targets []string) error {
	fmt.Println("digraph mk {")
	fmt.Println("  rankdir=LR;")
	visited := map[string]bool{}
	for _, t := range targets {
		if err := g.printGraph(t, visited); err != nil {
			return err
		}
	}
	fmt.Println("}")
	return nil
}

func (g *Graph) printGraph(target string, visited map[string]bool) error {
	if visited[target] {
		return nil
	}
	visited[target] = true

	rule, err := g.Resolve(target)
	if err != nil {
		return err
	}

	if rule.isTask {
		fmt.Printf("  %q [shape=box];\n", target)
	}

	for _, p := range rule.prereqs {
		fmt.Printf("  %q -> %q;\n", target, p)
		if err := g.printGraph(p, visited); err != nil {
			return err
		}
	}
	return nil
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
