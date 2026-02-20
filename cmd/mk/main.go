package main

import (
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
	)
	flag.Parse()

	if err := run(*file, *verbose, flag.Args()); err != nil {
		fmt.Fprintf(os.Stderr, "mk: %s\n", err)
		os.Exit(1)
	}
}

func run(file string, verbose bool, targets []string) error {
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

	exec := mk.NewExecutor(graph, state, vars, verbose)

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

	return state.Save()
}
