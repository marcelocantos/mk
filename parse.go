package mk

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Parse parses an mkfile from a reader.
func Parse(r io.Reader) (*File, error) {
	// Read all lines upfront so we can peek/backtrack.
	var rawLines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		rawLines = append(rawLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Join line continuations: lines ending with \ are merged with the next.
	var lines []string
	for i := 0; i < len(rawLines); i++ {
		line := rawLines[i]
		for strings.HasSuffix(line, "\\") && i+1 < len(rawLines) {
			line = line[:len(line)-1] + rawLines[i+1]
			i++
		}
		lines = append(lines, line)
	}

	p := &parser{lines: lines}
	stmts, err := p.parseBlock(false)
	if err != nil {
		return nil, err
	}
	return &File{Stmts: stmts}, nil
}

type parser struct {
	lines []string
	pos   int
}

func (p *parser) peek() (string, bool) {
	if p.pos >= len(p.lines) {
		return "", false
	}
	return p.lines[p.pos], true
}

func (p *parser) next() (string, int, bool) {
	if p.pos >= len(p.lines) {
		return "", 0, false
	}
	line := p.lines[p.pos]
	lineNum := p.pos + 1
	p.pos++
	return line, lineNum, true
}

func (p *parser) parseBlock(inConditional bool) ([]Node, error) {
	var stmts []Node
	for {
		line, ok := p.peek()
		if !ok {
			break
		}
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			p.pos++
			continue
		}

		// End of conditional block
		if inConditional && (trimmed == "end" || trimmed == "else" || strings.HasPrefix(trimmed, "elif ")) {
			break
		}

		// Indented line outside a rule
		if line[0] == ' ' || line[0] == '\t' {
			if inConditional {
				// Inside a conditional, indented lines are the body
				trimmed = strings.TrimSpace(line)
			} else {
				return nil, fmt.Errorf("line %d: unexpected indented line outside a rule", p.pos+1)
			}
		}

		node, err := p.parseStatement(trimmed)
		if err != nil {
			return nil, err
		}
		if node != nil {
			stmts = append(stmts, node)
		}
	}
	return stmts, nil
}

func (p *parser) parseStatement(trimmed string) (Node, error) {
	_, lineNum, _ := p.next() // consume the line

	// Include
	if strings.HasPrefix(trimmed, "include ") {
		n, err := parseInclude(trimmed, lineNum)
		return n, err
	}

	// Conditional
	if strings.HasPrefix(trimmed, "if ") {
		return p.parseConditional(trimmed, lineNum)
	}

	// Lazy variable
	if rest, ok := strings.CutPrefix(trimmed, "lazy "); ok {
		if name, value, ok := parseAssign(rest); ok {
			return VarAssign{Name: name, Op: OpSet, Value: value, Lazy: true, Line: lineNum}, nil
		}
	}

	// Variable assignment
	if name, value, ok := parseAssign(trimmed); ok {
		return VarAssign{Name: name, Op: OpSet, Value: value, Line: lineNum}, nil
	}
	if name, value, ok := parseAppend(trimmed); ok {
		return VarAssign{Name: name, Op: OpAppend, Value: value, Line: lineNum}, nil
	}
	if name, value, ok := parseCondAssign(trimmed); ok {
		return VarAssign{Name: name, Op: OpCondSet, Value: value, Line: lineNum}, nil
	}

	// Rule or task
	if isTask, keep, targets, prereqs, orderOnly, ok := parseRuleHeader(trimmed); ok {
		recipe := p.parseRecipe()
		return Rule{
			Targets:          targets,
			Prereqs:          prereqs,
			OrderOnlyPrereqs: orderOnly,
			Recipe:           recipe,
			IsTask:           isTask,
			Keep:             keep,
			Line:             lineNum,
		}, nil
	}

	return nil, fmt.Errorf("line %d: unrecognized syntax: %s", lineNum, trimmed)
}

func (p *parser) parseRecipe() []string {
	var lines []string
	for {
		line, ok := p.peek()
		if !ok {
			break
		}
		if line == "" {
			p.pos++
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			break
		}
		p.pos++
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines
}

func (p *parser) parseConditional(line string, lineNum int) (Node, error) {
	cond := Conditional{Line: lineNum}
	branch, err := parseCondExpr(line)
	if err != nil {
		return nil, fmt.Errorf("line %d: %w", lineNum, err)
	}

	for {
		body, err := p.parseBlock(true)
		if err != nil {
			return nil, err
		}
		branch.Body = body
		cond.Branches = append(cond.Branches, branch)

		termLine, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("line %d: unexpected end of file in conditional", lineNum)
		}
		termTrimmed := strings.TrimSpace(termLine)
		p.pos++ // consume the terminator

		if termTrimmed == "end" {
			break
		}

		branch, err = parseCondExpr(termTrimmed)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", p.pos, err)
		}
	}

	return cond, nil
}

func parseAssign(line string) (string, string, bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == '=' && (i == 0 || line[i-1] != '+' && line[i-1] != '!' && line[i-1] != '?') {
			prefix := line[:i]
			if strings.ContainsRune(prefix, ':') {
				return "", "", false
			}
			name := strings.TrimSpace(prefix)
			value := strings.TrimSpace(line[i+1:])
			if isValidVarName(name) {
				return name, value, true
			}
			return "", "", false
		}
	}
	return "", "", false
}

func parseCondAssign(line string) (string, string, bool) {
	idx := strings.Index(line, "?=")
	if idx < 0 {
		return "", "", false
	}
	prefix := line[:idx]
	if strings.ContainsRune(prefix, ':') {
		return "", "", false
	}
	name := strings.TrimSpace(prefix)
	value := strings.TrimSpace(line[idx+2:])
	if isValidVarName(name) {
		return name, value, true
	}
	return "", "", false
}

func parseAppend(line string) (string, string, bool) {
	idx := strings.Index(line, "+=")
	if idx < 0 {
		return "", "", false
	}
	prefix := line[:idx]
	if strings.ContainsRune(prefix, ':') {
		return "", "", false
	}
	name := strings.TrimSpace(prefix)
	value := strings.TrimSpace(line[idx+2:])
	if isValidVarName(name) {
		return name, value, true
	}
	return "", "", false
}

func parseRuleHeader(line string) (isTask, keep bool, targets, prereqs, orderOnlyPrereqs []string, ok bool) {
	if strings.HasPrefix(line, "!") {
		isTask = true
		line = line[1:]
	}

	colonIdx := strings.IndexByte(line, ':')
	if colonIdx < 0 {
		return false, false, nil, nil, nil, false
	}

	targetStr := strings.TrimSpace(line[:colonIdx])
	prereqStr := strings.TrimSpace(line[colonIdx+1:])

	if targetStr == "" {
		return false, false, nil, nil, nil, false
	}

	// Check for [keep] annotation
	if strings.HasSuffix(targetStr, "[keep]") {
		keep = true
		targetStr = strings.TrimSpace(strings.TrimSuffix(targetStr, "[keep]"))
	}

	targets = strings.Fields(targetStr)

	// Split prereqs on | for order-only prerequisites
	normalStr, orderOnlyStr, _ := strings.Cut(prereqStr, "|")
	if s := strings.TrimSpace(normalStr); s != "" {
		prereqs = strings.Fields(s)
	}
	if s := strings.TrimSpace(orderOnlyStr); s != "" {
		orderOnlyPrereqs = strings.Fields(s)
	}

	return isTask, keep, targets, prereqs, orderOnlyPrereqs, true
}

func parseInclude(line string, lineNum int) (Node, error) {
	rest := strings.TrimPrefix(line, "include ")
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return nil, fmt.Errorf("line %d: include requires a path", lineNum)
	}

	inc := Include{Path: parts[0], Line: lineNum}
	if len(parts) >= 3 && parts[1] == "as" {
		inc.Alias = parts[2]
	}
	return inc, nil
}

func parseCondExpr(line string) (CondBranch, error) {
	if line == "else" {
		return CondBranch{Op: "else"}, nil
	}

	var rest, op string
	if after, ok := strings.CutPrefix(line, "if "); ok {
		rest, op = after, "if"
	} else if after, ok := strings.CutPrefix(line, "elif "); ok {
		rest, op = after, "elif"
	} else {
		return CondBranch{}, fmt.Errorf("expected if/elif/else, got: %s", line)
	}

	if parts := strings.SplitN(rest, " == ", 2); len(parts) == 2 {
		return CondBranch{Op: op, Left: strings.TrimSpace(parts[0]), Cmp: "==", Right: strings.TrimSpace(parts[1])}, nil
	}
	if parts := strings.SplitN(rest, " != ", 2); len(parts) == 2 {
		return CondBranch{Op: op, Left: strings.TrimSpace(parts[0]), Cmp: "!=", Right: strings.TrimSpace(parts[1])}, nil
	}

	return CondBranch{}, fmt.Errorf("expected comparison (== or !=), got: %s", rest)
}

func isValidVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
				return false
			}
		} else {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
				return false
			}
		}
	}
	return true
}
