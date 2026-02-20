package mk

// Node is the interface for all AST nodes.
type Node interface {
	node()
}

// File represents a parsed mkfile.
type File struct {
	Stmts []Node
}

// VarAssign represents a variable assignment: name = value, name += value, lazy name = value.
type VarAssign struct {
	Name   string
	Op     AssignOp
	Value  string
	Lazy   bool
	Line   int
}

type AssignOp int

const (
	OpSet     AssignOp = iota // =
	OpAppend                  // +=
	OpCondSet                 // ?=
)

// Rule represents a build rule: targets: prerequisites \n recipe.
type Rule struct {
	Targets          []string
	Prereqs          []string
	OrderOnlyPrereqs []string // after |
	Recipe           []string
	IsTask           bool // ! prefix
	Keep             bool // [keep] annotation
	Line             int
}

// Include represents an include directive.
type Include struct {
	Path  string
	Alias string // "as foo" scoping
	Line  int
}

// Conditional represents if/elif/else/end blocks.
type Conditional struct {
	Branches []CondBranch
	Line     int
}

type CondBranch struct {
	Op    string // "if", "elif", "else"
	Left  string
	Cmp   string // "==", "!="
	Right string
	Body  []Node
}

func (VarAssign) node()   {}
func (Rule) node()        {}
func (Include) node()     {}
func (Conditional) node() {}
