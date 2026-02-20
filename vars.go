package mk

import (
	"os"
	"strings"
)

// Vars is a variable store. All variables are also environment variables.
type Vars struct {
	vals map[string]string
	lazy map[string]string // unevaluated lazy expressions
}

func NewVars() *Vars {
	v := &Vars{
		vals: make(map[string]string),
		lazy: make(map[string]string),
	}
	// Import environment
	for _, env := range os.Environ() {
		k, val, ok := strings.Cut(env, "=")
		if ok {
			v.vals[k] = val
		}
	}
	return v
}

// Set sets a variable immediately.
func (v *Vars) Set(name, value string) {
	v.vals[name] = value
	delete(v.lazy, name)
}

// SetLazy sets a variable for deferred evaluation.
func (v *Vars) SetLazy(name, expr string) {
	v.lazy[name] = expr
	delete(v.vals, name)
}

// Append appends to a variable.
func (v *Vars) Append(name, value string) {
	existing := v.Get(name)
	if existing != "" {
		v.Set(name, existing+" "+value)
	} else {
		v.Set(name, value)
	}
}

// Get retrieves a variable's value, evaluating lazy variables on demand.
func (v *Vars) Get(name string) string {
	if expr, ok := v.lazy[name]; ok {
		val := v.Expand(expr)
		v.vals[name] = val
		delete(v.lazy, name)
		return val
	}
	return v.vals[name]
}

// Expand expands variable references in a string.
// $name expands to the value of name.
// ${name} also works for delimiting.
// $$ expands to a literal $.
func (v *Vars) Expand(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++ // skip $
		if i >= len(s) {
			b.WriteByte('$')
			break
		}

		switch {
		case s[i] == '$':
			// $$ → literal $
			b.WriteByte('$')
			i++

		case s[i] == '{':
			// ${name}
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				b.WriteByte('$')
				b.WriteByte('{')
				i++
			} else {
				name := s[i+1 : i+end]
				b.WriteString(v.Get(name))
				i += end + 1
			}

		case s[i] == '(':
			// $(func args) — built-in functions
			end := findMatchingParen(s[i:])
			if end < 0 {
				b.WriteByte('$')
				b.WriteByte('(')
				i++
			} else {
				inner := s[i+1 : i+end]
				b.WriteString(v.evalFunc(inner))
				i += end + 1
			}

		case isIdentStart(s[i]):
			// $name or $name:old=new (substitution reference)
			start := i
			for i < len(s) && isIdentCont(s[i]) {
				i++
			}
			name := s[start:i]
			val := v.Get(name)

			// Check for substitution reference: $name:old=new
			if i < len(s) && s[i] == ':' {
				rest := s[i+1:]
				if eqIdx := strings.IndexByte(rest, '='); eqIdx >= 0 {
					// Find end of replacement (next space or end)
					endIdx := strings.IndexByte(rest[eqIdx+1:], ' ')
					var old, repl string
					if endIdx < 0 {
						old = rest[:eqIdx]
						repl = rest[eqIdx+1:]
						i = len(s)
					} else {
						old = rest[:eqIdx]
						repl = rest[eqIdx+1 : eqIdx+1+endIdx]
						i += 1 + eqIdx + 1 + endIdx
					}
					// Apply substitution: .c=.o means replace suffix .c with .o
					// Convert to % patterns for patsubstWord
					oldPat := "%" + old
					replPat := "%" + repl
					words := strings.Fields(val)
					for j, w := range words {
						words[j] = patsubstWord(oldPat, replPat, w)
					}
					val = strings.Join(words, " ")
				}
			}

			b.WriteString(val)

		default:
			b.WriteByte('$')
		}
	}
	return b.String()
}

// Environ returns the variables as environment strings for exec.
func (v *Vars) Environ() []string {
	var env []string
	for k, val := range v.vals {
		env = append(env, k+"="+val)
	}
	return env
}

// Clone creates a copy of the variable store.
func (v *Vars) Clone() *Vars {
	c := &Vars{
		vals: make(map[string]string, len(v.vals)),
		lazy: make(map[string]string, len(v.lazy)),
	}
	for k, val := range v.vals {
		c.vals[k] = val
	}
	for k, val := range v.lazy {
		c.lazy[k] = val
	}
	return c
}

func (v *Vars) evalFunc(inner string) string {
	name, args, _ := strings.Cut(inner, " ")
	switch name {
	case "wildcard":
		return v.funcWildcard(strings.TrimSpace(args))
	case "shell":
		return v.funcShell(strings.TrimSpace(args))
	case "patsubst":
		return v.funcPatsubst(strings.TrimSpace(args))
	default:
		// Unknown function, try as variable name
		return v.Get(inner)
	}
}

func (v *Vars) funcWildcard(pattern string) string {
	pattern = v.Expand(pattern)
	matches, err := wildcardGlob(pattern)
	if err != nil {
		return ""
	}
	return strings.Join(matches, " ")
}

func (v *Vars) funcShell(cmd string) string {
	cmd = v.Expand(cmd)
	out, err := runShellCapture(cmd)
	if err != nil {
		return ""
	}
	// Replace newlines with spaces, trim
	out = strings.ReplaceAll(strings.TrimSpace(out), "\n", " ")
	return out
}

func (v *Vars) funcPatsubst(args string) string {
	// $(patsubst pattern,replacement,text)
	parts := strings.SplitN(args, ",", 3)
	if len(parts) != 3 {
		return ""
	}
	pattern := strings.TrimSpace(parts[0])
	replacement := strings.TrimSpace(parts[1])
	text := strings.TrimSpace(v.Expand(parts[2]))

	words := strings.Fields(text)
	var result []string
	for _, w := range words {
		result = append(result, patsubstWord(pattern, replacement, w))
	}
	return strings.Join(result, " ")
}

func patsubstWord(pattern, replacement, word string) string {
	// Simple % substitution
	if !strings.Contains(pattern, "%") {
		if word == pattern {
			return replacement
		}
		return word
	}
	prefix, suffix, _ := strings.Cut(pattern, "%")
	if strings.HasPrefix(word, prefix) && strings.HasSuffix(word, suffix) {
		stem := word[len(prefix) : len(word)-len(suffix)]
		return strings.ReplaceAll(replacement, "%", stem)
	}
	return word
}

func findMatchingParen(s string) int {
	depth := 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}
