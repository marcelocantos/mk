package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/marcelocantos/mk"
)

func main() {
	var (
		file     = flag.String("f", "mkfile", "mkfile to read")
		verbose  = flag.Bool("v", false, "verbose output")
		force    = flag.Bool("B", false, "unconditional rebuild (ignore state)")
		dryRun   = flag.Bool("n", false, "dry run (print commands without executing)")
		why      = flag.Bool("why", false, "explain why targets are stale")
		graph    = flag.Bool("graph", false, "print dependency subgraph")
		showState = flag.Bool("state", false, "show build database entries")
	)
	flag.Parse()

	args := flag.Args()

	if err := run(*file, *verbose, *force, *dryRun, *why, *graph, *showState, args); err != nil {
		fmt.Fprintf(os.Stderr, "mk: %s\n", err)
		os.Exit(1)
	}
}

func run(file string, verbose, force, dryRun, why, graph, showState bool, targets []string) error {
	// Process command-line variable overrides
	vars := mk.NewVars()
	var buildTargets []string
	for _, arg := range targets {
		if name, value, ok := strings.Cut(arg, "="); ok {
			vars.Set(name, value)
		} else {
			buildTargets = append(buildTargets, arg)
		}
	}

	// --state only needs the build database
	if showState {
		state := mk.LoadState()
		if len(buildTargets) == 0 {
			return fmt.Errorf("--state requires at least one target")
		}
		for _, t := range buildTargets {
			ts := state.Targets[t]
			if ts == nil {
				fmt.Printf("no build state recorded for %q\n", t)
				continue
			}
			data, _ := json.MarshalIndent(ts, "", "  ")
			fmt.Printf("state for %q:\n%s\n", t, string(data))
		}
		return nil
	}

	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", file, err)
	}
	defer f.Close()

	ast, err := mk.Parse(f)
	if err != nil {
		return err
	}

	state := mk.LoadState()

	g, err := mk.BuildGraph(ast, vars, state)
	if err != nil {
		return err
	}

	if len(buildTargets) == 0 {
		def := g.DefaultTarget()
		if def == "" {
			return fmt.Errorf("no targets specified and no default target")
		}
		buildTargets = []string{def}
	}

	// --why: explain why targets are stale, then exit
	if why {
		for _, t := range buildTargets {
			reasons, err := g.WhyRebuild(t)
			if err != nil {
				return err
			}
			if len(reasons) == 0 {
				fmt.Printf("%s is up to date\n", t)
			} else {
				fmt.Printf("%s needs rebuilding:\n", t)
				for _, r := range reasons {
					fmt.Printf("  - %s\n", r)
				}
			}
		}
		return nil
	}

	// --graph: print dependency subgraph as DOT, then exit
	if graph {
		return g.PrintGraph(buildTargets)
	}

	// Normal build
	exec := mk.NewExecutor(g, state, vars, verbose, force, dryRun)

	for _, t := range buildTargets {
		if err := exec.Build(t); err != nil {
			return err
		}
	}

	if dryRun {
		return nil
	}
	return state.Save()
}
