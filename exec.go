package mk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Executor runs build recipes.
type Executor struct {
	graph   *Graph
	state   *BuildState
	vars    *Vars
	built   map[string]bool
	verbose bool
	force   bool // -B: unconditional rebuild
	dryRun  bool // -n: print commands without executing
}

func NewExecutor(graph *Graph, state *BuildState, vars *Vars, verbose, force, dryRun bool) *Executor {
	return &Executor{
		graph:   graph,
		state:   state,
		vars:    vars,
		built:   make(map[string]bool),
		verbose: verbose,
		force:   force,
		dryRun:  dryRun,
	}
}

// Build builds the given target and all its dependencies.
func (e *Executor) Build(target string) error {
	if e.built[target] {
		return nil
	}
	e.built[target] = true

	rule, err := e.graph.Resolve(target)
	if err != nil {
		return err
	}

	// Build prerequisites first
	for _, p := range rule.prereqs {
		if err := e.Build(p); err != nil {
			return fmt.Errorf("building %q for %q: %w", p, target, err)
		}
	}

	// No recipe = leaf node or prerequisite-only rule
	if len(rule.recipe) == 0 {
		return nil
	}

	// Check staleness
	recipeText := e.expandRecipe(rule, target)
	if !rule.isTask && !e.force && !e.state.IsStale(target, rule.prereqs, recipeText) {
		if e.verbose {
			fmt.Fprintf(os.Stderr, "mk: %q is up to date\n", target)
		}
		return nil
	}

	// Auto-create parent directory
	if !rule.isTask {
		dir := filepath.Dir(target)
		if dir != "." && dir != "" {
			if !e.dryRun {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("creating directory %q: %w", dir, err)
				}
			}
		}
	}

	// Execute recipe
	fmt.Fprintf(os.Stderr, "mk: building %q\n", target)
	if e.verbose || e.dryRun {
		for _, line := range strings.Split(recipeText, "\n") {
			fmt.Fprintf(os.Stderr, "  %s\n", line)
		}
	}
	if e.dryRun {
		return nil
	}
	if err := e.runRecipe(recipeText); err != nil {
		// Delete partial output on failure (for file targets), unless [keep]
		if !rule.isTask && !rule.keep {
			os.Remove(target)
		}
		return fmt.Errorf("recipe for %q failed: %w", target, err)
	}

	// Record successful build
	if !rule.isTask {
		e.state.Record(target, rule.prereqs, recipeText)
	}

	return nil
}

func (e *Executor) runRecipe(script string) error {
	fullScript := "set -e\n" + script

	cmd := exec.Command("sh", "-c", fullScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = e.vars.Environ()

	return cmd.Run()
}

func (e *Executor) expandRecipe(rule *resolvedRule, target string) string {
	vars := e.vars.Clone()
	vars.Set("target", target)
	if len(rule.prereqs) > 0 {
		vars.Set("input", rule.prereqs[0])
	}
	vars.Set("inputs", strings.Join(rule.prereqs, " "))

	// Set stem if available from pattern match
	if rule.stem != "" {
		vars.Set("stem", rule.stem)
	}

	// Find changed prerequisites
	var changed []string
	ts := e.state.Targets[target]
	for _, p := range rule.prereqs {
		if ts == nil {
			changed = append(changed, p)
			continue
		}
		h, err := hashFile(p)
		if err != nil || ts.InputHashes[p] != h {
			changed = append(changed, p)
		}
	}
	vars.Set("changed", strings.Join(changed, " "))

	var lines []string
	for _, line := range rule.recipe {
		ignoreErr := false
		l := line
		for len(l) > 0 && (l[0] == '@' || l[0] == '-') {
			if l[0] == '-' {
				ignoreErr = true
			}
			l = l[1:]
		}

		expanded := vars.Expand(l)
		if ignoreErr {
			expanded += " || true"
		}
		lines = append(lines, expanded)
	}

	return strings.Join(lines, "\n")
}
