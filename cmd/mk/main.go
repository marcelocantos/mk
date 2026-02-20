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
		file    = flag.String("f", "mkfile", "mkfile to read")
		verbose = flag.Bool("v", false, "verbose output")
		force   = flag.Bool("B", false, "unconditional rebuild (ignore state)")
		dryRun  = flag.Bool("n", false, "dry run (print commands without executing)")
	)
	flag.Parse()

	args := flag.Args()

	// Subcommands
	if len(args) > 0 {
		switch args[0] {
		case "why":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "mk why: requires a target\n")
				os.Exit(1)
			}
			if err := cmdWhy(*file, args[1]); err != nil {
				fmt.Fprintf(os.Stderr, "mk: %s\n", err)
				os.Exit(1)
			}
			return
		case "state":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "mk state: requires a target\n")
				os.Exit(1)
			}
			cmdState(args[1])
			return
		}
	}

	if err := run(*file, *verbose, *force, *dryRun, args); err != nil {
		fmt.Fprintf(os.Stderr, "mk: %s\n", err)
		os.Exit(1)
	}
}

func run(file string, verbose, force, dryRun bool, targets []string) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", file, err)
	}
	defer f.Close()

	ast, err := mk.Parse(f)
	if err != nil {
		return err
	}

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

	state := mk.LoadState()

	graph, err := mk.BuildGraph(ast, vars, state)
	if err != nil {
		return err
	}

	exec := mk.NewExecutor(graph, state, vars, verbose, force, dryRun)

	if len(buildTargets) == 0 {
		def := graph.DefaultTarget()
		if def == "" {
			return fmt.Errorf("no targets specified and no default target")
		}
		buildTargets = []string{def}
	}

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

func cmdWhy(file, target string) error {
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", file, err)
	}
	defer f.Close()

	ast, err := mk.Parse(f)
	if err != nil {
		return err
	}

	vars := mk.NewVars()
	state := mk.LoadState()
	graph, err := mk.BuildGraph(ast, vars, state)
	if err != nil {
		return err
	}

	reasons, err := graph.WhyRebuild(target)
	if err != nil {
		return err
	}

	if len(reasons) == 0 {
		fmt.Printf("%s is up to date\n", target)
	} else {
		fmt.Printf("%s needs rebuilding:\n", target)
		for _, r := range reasons {
			fmt.Printf("  - %s\n", r)
		}
	}
	return nil
}

func cmdState(target string) {
	state := mk.LoadState()
	ts := state.Targets[target]
	if ts == nil {
		fmt.Printf("no build state recorded for %q\n", target)
		return
	}
	data, _ := json.MarshalIndent(ts, "", "  ")
	fmt.Printf("state for %q:\n%s\n", target, string(data))
}
