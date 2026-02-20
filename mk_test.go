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
	input := `lazy version = $(shell echo hello)`
	f, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	v := f.Stmts[0].(VarAssign)
	if !v.Lazy {
		t.Error("expected lazy")
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

	// Write mkfile to disk for $(wildcard) etc.
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
}
