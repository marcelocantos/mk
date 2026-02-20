package mk

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Executor runs build recipes.
type Executor struct {
	graph   *Graph
	state   *BuildState
	vars    *Vars
	verbose bool
	force   bool // -B: unconditional rebuild
	dryRun  bool // -n: print commands without executing
	jobs    int  // max concurrent recipes (0 = unlimited)

	mu       sync.Mutex
	building map[string]*buildResult // singleflight dedup
	sem      chan struct{}            // recipe concurrency limiter; nil = unlimited
	outputMu sync.Mutex              // serializes buffered output flushes
	cache    *HashCache              // file content hash cache
}

// buildResult tracks the in-progress or completed build of a target.
// Multiple targets from the same multi-output rule share one buildResult.
type buildResult struct {
	done chan struct{}
	err  error
}

func NewExecutor(graph *Graph, state *BuildState, vars *Vars, verbose, force, dryRun bool, jobs int) *Executor {
	if jobs < 0 {
		jobs = runtime.NumCPU()
	}

	var sem chan struct{}
	if jobs > 0 {
		sem = make(chan struct{}, jobs)
	}
	// jobs == 0: sem stays nil → unlimited concurrency

	return &Executor{
		graph:    graph,
		state:    state,
		vars:     vars,
		verbose:  verbose,
		force:    force,
		dryRun:   dryRun,
		jobs:     jobs,
		building: make(map[string]*buildResult),
		sem:      sem,
		cache:    NewHashCache(),
	}
}

// Build builds the given target and all its dependencies.
// Safe to call concurrently from multiple goroutines.
func (e *Executor) Build(target string) error {
	e.mu.Lock()
	if res, ok := e.building[target]; ok {
		e.mu.Unlock()
		<-res.done
		return res.err
	}

	// Resolve rule under lock to discover co-targets for multi-output dedup.
	// Graph.Resolve is read-only and safe to call here.
	rule, err := e.graph.Resolve(target)
	if err != nil {
		e.mu.Unlock()
		return err
	}

	res := &buildResult{done: make(chan struct{})}
	for _, t := range rule.targets {
		e.building[t] = res
	}
	e.mu.Unlock()

	err = e.doBuild(target, rule)
	res.err = err
	close(res.done)
	return err
}

func (e *Executor) doBuild(target string, rule *resolvedRule) error {
	// Build all prerequisites concurrently
	allPrereqs := make([]string, 0, len(rule.prereqs)+len(rule.orderOnlyPrereqs))
	allPrereqs = append(allPrereqs, rule.prereqs...)
	allPrereqs = append(allPrereqs, rule.orderOnlyPrereqs...)

	errs := make([]error, len(allPrereqs))
	var wg sync.WaitGroup
	for i, p := range allPrereqs {
		wg.Add(1)
		go func(idx int, prereq string) {
			defer wg.Done()
			errs[idx] = e.Build(prereq)
		}(i, p)
	}
	wg.Wait()

	// Check for prereq errors
	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("building %q for %q: %w", allPrereqs[i], target, err)
		}
	}

	// No recipe = leaf node or prerequisite-only rule
	if len(rule.recipe) == 0 {
		return nil
	}

	// Check staleness (only normal prereqs affect staleness)
	recipeText := e.expandRecipe(rule)
	fingerprint := e.expandFingerprint(rule)
	if !rule.isTask && !e.force && !e.state.IsStale(rule.targets, rule.prereqs, recipeText, fingerprint, e.cache) {
		if e.verbose {
			e.outputMu.Lock()
			fmt.Fprintf(os.Stderr, "mk: %q is up to date\n", rule.target)
			e.outputMu.Unlock()
		}
		return nil
	}

	// Acquire semaphore slot to limit concurrent recipes
	if e.sem != nil {
		e.sem <- struct{}{}
		defer func() { <-e.sem }()
	}

	return e.executeRecipe(rule, recipeText, fingerprint)
}

func (e *Executor) executeRecipe(rule *resolvedRule, recipeText, fingerprint string) error {
	// Auto-create parent directories for all targets
	if !rule.isTask {
		for _, t := range rule.targets {
			dir := filepath.Dir(t)
			if dir != "." && dir != "" {
				if !e.dryRun {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return fmt.Errorf("creating directory %q: %w", dir, err)
					}
				}
			}
		}
	}

	// Build banner
	var banner strings.Builder
	fmt.Fprintf(&banner, "mk: building %q\n", rule.target)
	if e.verbose || e.dryRun {
		for _, line := range strings.Split(recipeText, "\n") {
			fmt.Fprintf(&banner, "  %s\n", line)
		}
	}

	if e.dryRun {
		e.outputMu.Lock()
		fmt.Fprint(os.Stderr, banner.String())
		e.outputMu.Unlock()
		return nil
	}

	// Determine output mode: serial streams directly, parallel buffers
	serial := e.sem != nil && cap(e.sem) == 1
	var stdout, stderr io.Writer
	var outBuf, errBuf bytes.Buffer

	if serial {
		// Serial mode: stream banner and output directly
		e.outputMu.Lock()
		fmt.Fprint(os.Stderr, banner.String())
		e.outputMu.Unlock()
		stdout = os.Stdout
		stderr = os.Stderr
	} else {
		// Parallel mode: buffer output, flush atomically on completion
		stdout = &outBuf
		stderr = &errBuf
	}

	// Execute recipe
	fullScript := "set -e\n" + recipeText
	cmd := exec.Command("sh", "-c", fullScript)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = e.vars.Environ()

	err := cmd.Run()

	if !serial {
		// Flush buffered output atomically
		e.outputMu.Lock()
		fmt.Fprint(os.Stderr, banner.String())
		outBuf.WriteTo(os.Stdout)
		errBuf.WriteTo(os.Stderr)
		e.outputMu.Unlock()
	}

	if err != nil {
		// Delete partial output on failure (for file targets), unless [keep]
		if !rule.isTask && !rule.keep {
			for _, t := range rule.targets {
				os.Remove(t)
			}
		}
		return fmt.Errorf("recipe for %q failed: %w", rule.target, err)
	}

	// Record successful build for all outputs
	if !rule.isTask {
		e.state.Record(rule.targets, rule.prereqs, recipeText, fingerprint, e.cache)
	}

	return nil
}

func (e *Executor) expandFingerprint(rule *resolvedRule) string {
	if rule.fingerprint == "" {
		return ""
	}
	vars := e.vars.Clone()
	vars.Set("target", rule.target)
	if len(rule.prereqs) > 0 {
		vars.Set("input", rule.prereqs[0])
	}
	vars.Set("inputs", strings.Join(rule.prereqs, " "))
	if rule.stem != "" {
		vars.Set("stem", rule.stem)
	}
	return vars.Expand(rule.fingerprint)
}

func (e *Executor) expandRecipe(rule *resolvedRule) string {
	vars := e.vars.Clone()
	vars.Set("target", rule.target)
	if len(rule.prereqs) > 0 {
		vars.Set("input", rule.prereqs[0])
	}
	vars.Set("inputs", strings.Join(rule.prereqs, " "))

	// Set stem if available from pattern match
	if rule.stem != "" {
		vars.Set("stem", rule.stem)
	}

	// Find changed prerequisites (only normal prereqs)
	var changed []string
	ts := e.state.GetTarget(rule.target)
	for _, p := range rule.prereqs {
		if ts == nil {
			changed = append(changed, p)
			continue
		}
		h, err := e.cache.Hash(p)
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
