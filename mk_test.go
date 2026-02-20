package mk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVariables(t *testing.T) {
	input := `
cc = gcc
cflags = -Wall -O2
cflags += -Werror
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.Stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(f.Stmts))
	}

	v1 := f.Stmts[0].(VarAssign)
	if v1.Name != "cc" || v1.Value != "gcc" || v1.Op != OpSet {
		t.Errorf("unexpected var: %+v", v1)
	}

	v2 := f.Stmts[1].(VarAssign)
	if v2.Name != "cflags" || v2.Value != "-Wall -O2" || v2.Op != OpSet {
		t.Errorf("unexpected var: %+v", v2)
	}

	v3 := f.Stmts[2].(VarAssign)
	if v3.Name != "cflags" || v3.Value != "-Werror" || v3.Op != OpAppend {
		t.Errorf("unexpected var: %+v", v3)
	}
}

func TestParseLazy(t *testing.T) {
	input := `lazy version = $[shell echo hello]`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	v := f.Stmts[0].(VarAssign)
	if !v.Lazy {
		t.Error("expected lazy")
	}
}

func TestParseCondAssign(t *testing.T) {
	input := `cc ?= gcc`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	v := f.Stmts[0].(VarAssign)
	if v.Name != "cc" || v.Value != "gcc" || v.Op != OpCondSet {
		t.Errorf("unexpected var: %+v", v)
	}
}

func TestCondAssignSemantics(t *testing.T) {
	input := `
cc = clang
cc ?= gcc
opt ?= O2
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	vars := NewVars()
	state := &BuildState{Targets: make(map[string]*TargetState)}
	_, err = BuildGraph(f, vars, state)
	if err != nil {
		t.Fatal(err)
	}

	// cc was already set, ?= should not overwrite
	if got := vars.Get("cc"); got != "clang" {
		t.Errorf("cc = %q, want %q", got, "clang")
	}
	// opt was not set, ?= should set it
	if got := vars.Get("opt"); got != "O2" {
		t.Errorf("opt = %q, want %q", got, "O2")
	}
}

func TestParseRule(t *testing.T) {
	input := `
build/{name}.o: src/{name}.c
    $cc $cflags -c $input -o $target
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if len(r.Targets) != 1 || r.Targets[0] != "build/{name}.o" {
		t.Errorf("unexpected targets: %v", r.Targets)
	}
	if len(r.Prereqs) != 1 || r.Prereqs[0] != "src/{name}.c" {
		t.Errorf("unexpected prereqs: %v", r.Prereqs)
	}
	if len(r.Recipe) != 1 {
		t.Errorf("expected 1 recipe line, got %d", len(r.Recipe))
	}
	if r.IsTask {
		t.Error("should not be a task")
	}
}

func TestParseTask(t *testing.T) {
	input := `
!clean:
    rm -rf build/
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	r := f.Stmts[0].(Rule)
	if !r.IsTask {
		t.Error("should be a task")
	}
	if r.Targets[0] != "clean" {
		t.Errorf("expected target 'clean', got %q", r.Targets[0])
	}
}

func TestParseConditional(t *testing.T) {
	input := `
if $cc == gcc
    cflags += -Wextra
end
`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	c := f.Stmts[0].(Conditional)
	if len(c.Branches) != 1 {
		t.Fatalf("expected 1 branch, got %d", len(c.Branches))
	}
	if c.Branches[0].Left != "$cc" {
		t.Errorf("expected left '$cc', got %q", c.Branches[0].Left)
	}
}

func TestVarExpansion(t *testing.T) {
	v := NewVars()
	v.Set("name", "world")
	v.Set("greeting", "hello")

	tests := []struct {
		input string
		want  string
	}{
		{"$name", "world"},
		{"${name}", "world"},
		{"$greeting $name", "hello world"},
		{"${greeting}_${name}", "hello_world"},
		{"no vars here", "no vars here"},
		{"$$literal", "$literal"},
	}

	for _, tt := range tests {
		got := v.Expand(tt.input)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSubstitutionRef(t *testing.T) {
	v := NewVars()
	v.Set("src", "foo.c bar.c baz.c")

	got := v.Expand("$src:.c=.o")
	if got != "foo.o bar.o baz.o" {
		t.Errorf("substitution ref = %q, want %q", got, "foo.o bar.o baz.o")
	}
}

func TestFuncSyntax(t *testing.T) {
	v := NewVars()
	v.Set("files", "foo.c bar.h baz.c")

	// $[...] should invoke mk functions
	got := v.Expand("$[filter %.c,$files]")
	if got != "foo.c baz.c" {
		t.Errorf("$[filter] = %q, want %q", got, "foo.c baz.c")
	}

	// $(...) should pass through for shell use
	got = v.Expand("echo $(date)")
	if got != "echo $(date)" {
		t.Errorf("$(...) should pass through, got %q", got)
	}
}

func TestBuiltinFunctions(t *testing.T) {
	v := NewVars()
	v.Set("src", "foo.c bar.c baz.c")
	v.Set("objs", "foo.o bar.o baz.o")
	v.Set("files", "main.c lib.c main.h lib.h")

	tests := []struct {
		input string
		want  string
	}{
		// subst
		{"$[subst .c,.o,$src]", "foo.o bar.o baz.o"},
		// filter
		{"$[filter %.c,$files]", "main.c lib.c"},
		// filter-out
		{"$[filter-out %.h,$files]", "main.c lib.c"},
		// dir
		{"$[dir src/foo.c]", "src/"},
		{"$[dir foo.c]", "./"},
		// notdir
		{"$[notdir src/foo.c]", "foo.c"},
		// basename
		{"$[basename src/foo.c]", "src/foo"},
		// suffix
		{"$[suffix foo.c bar.h]", ".c .h"},
		// addprefix
		{"$[addprefix src/,$src]", "src/foo.c src/bar.c src/baz.c"},
		// addsuffix
		{"$[addsuffix .bak,$objs]", "foo.o.bak bar.o.bak baz.o.bak"},
		// sort (also deduplicates)
		{"$[sort c b a b]", "a b c"},
		// word
		{"$[word 2,$src]", "bar.c"},
		// words
		{"$[words $src]", "3"},
		// strip
		{"$[strip  foo   bar  ]", "foo bar"},
		// findstring
		{"$[findstring bar,$src]", "bar"},
		{"$[findstring xyz,$src]", ""},
		// if
		{"$[if yes,true,false]", "true"},
		{"$[if ,true,false]", "false"},
		{"$[if yes,true]", "true"},
		{"$[if ,true]", ""},
		// patsubst
		{"$[patsubst %.c,%.o,$src]", "foo.o bar.o baz.o"},
	}

	for _, tt := range tests {
		got := v.Expand(tt.input)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestVarProperties(t *testing.T) {
	v := NewVars()
	v.Set("src", "src/main.c")

	tests := []struct {
		input string
		want  string
	}{
		{"$src.dir", "src"},
		{"$src.file", "main.c"},
	}

	for _, tt := range tests {
		got := v.Expand(tt.input)
		if got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLineContinuation(t *testing.T) {
	input := "cflags = -Wall \\\n-O2 \\\n-Werror\n"
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.Stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(f.Stmts))
	}
	v := f.Stmts[0].(VarAssign)
	if v.Value != "-Wall -O2 -Werror" {
		t.Errorf("line continuation: got %q, want %q", v.Value, "-Wall -O2 -Werror")
	}
}

func TestEndToEnd(t *testing.T) {
	// Set up a temporary directory
	dir := t.TempDir()

	// Create source files
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "main.c"), []byte(`int main() { return 0; }`), 0o644)

	// Create mkfile
	mkfile := `
cc = cc
src = src/main.c
obj = $src:.c=.o

build/{name}.o: src/{name}.c
    $cc -c $input -o $target

build/app: $obj
    $cc -o $target $inputs

!clean:
    rm -rf build/ .mk/
`
	// Parse
	f, err := Parse(strings.NewReader(mkfile))
	if err != nil {
		t.Fatal(err)
	}

	// Build graph
	vars := NewVars()
	// Override working directory context
	oldDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldDir)

	// Write mkfile to disk for $[wildcard] etc.
	os.WriteFile(filepath.Join(dir, "mkfile"), []byte(mkfile), 0o644)

	state := &BuildState{Targets: make(map[string]*TargetState)}
	graph, err := BuildGraph(f, vars, state)
	if err != nil {
		t.Fatal(err)
	}

	// Verify default target
	def := graph.DefaultTarget()
	if def != "build/app" {
		t.Errorf("default target = %q, want %q", def, "build/app")
	}

	// Verify pattern resolution
	rule, err := graph.Resolve("build/main.o")
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.prereqs) != 1 || rule.prereqs[0] != "src/main.c" {
		t.Errorf("prereqs = %v, want [src/main.c]", rule.prereqs)
	}
	if rule.stem != "main" {
		t.Errorf("stem = %q, want %q", rule.stem, "main")
	}
}

func TestWhyStale(t *testing.T) {
	state := &BuildState{Targets: make(map[string]*TargetState)}

	// No previous build
	reasons := state.WhyStale("foo", []string{"bar"}, "recipe")
	if len(reasons) != 1 || reasons[0] != "no previous build recorded" {
		t.Errorf("WhyStale = %v, want [no previous build recorded]", reasons)
	}
}
