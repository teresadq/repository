package security

import (
	"fmt"
	"regexp"
	"strings"
)

// SelectorContext is evaluated against a content selector expression.
type SelectorContext struct {
	Path       string
	Format     string
	Repository string
}

// ValidateSelector parses an expression and evaluates it against a sample
// context to catch syntax and regexp errors at save time.
func ValidateSelector(expression string) error {
	_, err := MatchSelector(expression, SelectorContext{Path: "sample/path", Format: "raw", Repository: "repo"})
	return err
}

// MatchSelector evaluates a content selector expression. Supported syntax:
//
//	path == "com/acme" && (format == "maven" || repository == "releases")
//	path.startsWith("com/acme/") && !path.endsWith(".tmp")
//
// Fields: path, format, repository (repo alias). Operators: ==, !=,
// startsWith, endsWith, contains, matches. Boolean: &&, ||, !, parentheses.
func MatchSelector(expression string, context SelectorContext) (bool, error) {
	tokens, err := tokenizeSelector(expression)
	if err != nil {
		return false, err
	}
	parser := &selectorParser{tokens: tokens, context: context}
	result, err := parser.parseOr()
	if err != nil {
		return false, err
	}
	if parser.index != len(parser.tokens) {
		return false, fmt.Errorf("unexpected token: %s", parser.tokens[parser.index].value)
	}
	return result, nil
}

type selectorToken struct {
	kind  string // ident, string, op
	value string
}

func tokenizeSelector(expression string) ([]selectorToken, error) {
	tokens := make([]selectorToken, 0)
	for index := 0; index < len(expression); {
		ch := expression[index]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			index++
		case ch == '"' || ch == '\'':
			quote := ch
			index++
			var builder strings.Builder
			for index < len(expression) && expression[index] != quote {
				if expression[index] == '\\' && index+1 < len(expression) {
					index++
				}
				builder.WriteByte(expression[index])
				index++
			}
			if index >= len(expression) {
				return nil, fmt.Errorf("unterminated string")
			}
			index++
			tokens = append(tokens, selectorToken{kind: "string", value: builder.String()})
		case ch == '&' || ch == '|':
			if index+1 >= len(expression) || expression[index+1] != ch {
				return nil, fmt.Errorf("invalid operator at %d", index)
			}
			tokens = append(tokens, selectorToken{kind: "op", value: string(ch) + string(ch)})
			index += 2
		case ch == '=' || ch == '!':
			if index+1 < len(expression) && expression[index+1] == '=' {
				tokens = append(tokens, selectorToken{kind: "op", value: expression[index : index+2]})
				index += 2
				continue
			}
			if ch == '!' {
				tokens = append(tokens, selectorToken{kind: "op", value: "!"})
				index++
				continue
			}
			return nil, fmt.Errorf("invalid operator at %d", index)
		case ch == '(' || ch == ')' || ch == '.':
			tokens = append(tokens, selectorToken{kind: "op", value: string(ch)})
			index++
		case isSelectorIdent(ch):
			start := index
			for index < len(expression) && isSelectorIdent(expression[index]) {
				index++
			}
			tokens = append(tokens, selectorToken{kind: "ident", value: expression[start:index]})
		default:
			return nil, fmt.Errorf("unexpected character %q", ch)
		}
	}
	return tokens, nil
}

func isSelectorIdent(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_'
}

type selectorParser struct {
	tokens  []selectorToken
	index   int
	context SelectorContext
}

func (p *selectorParser) peek() selectorToken {
	if p.index >= len(p.tokens) {
		return selectorToken{}
	}
	return p.tokens[p.index]
}

func (p *selectorParser) parseOr() (bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return false, err
	}
	for p.peek().value == "||" {
		p.index++
		right, err := p.parseAnd()
		if err != nil {
			return false, err
		}
		left = left || right
	}
	return left, nil
}

func (p *selectorParser) parseAnd() (bool, error) {
	left, err := p.parseUnary()
	if err != nil {
		return false, err
	}
	for p.peek().value == "&&" {
		p.index++
		right, err := p.parseUnary()
		if err != nil {
			return false, err
		}
		left = left && right
	}
	return left, nil
}

func (p *selectorParser) parseUnary() (bool, error) {
	if p.peek().value == "!" {
		p.index++
		value, err := p.parseUnary()
		if err != nil {
			return false, err
		}
		return !value, nil
	}
	if p.peek().value == "(" {
		p.index++
		value, err := p.parseOr()
		if err != nil {
			return false, err
		}
		if p.peek().value != ")" {
			return false, fmt.Errorf("missing closing parenthesis")
		}
		p.index++
		return value, nil
	}
	return p.parseComparison()
}

func (p *selectorParser) parseComparison() (bool, error) {
	token := p.peek()
	if token.kind != "ident" {
		return false, fmt.Errorf("expected field, got %q", token.value)
	}
	switch token.value {
	case "path", "format", "repository", "repo":
	default:
		return false, fmt.Errorf("unknown selector field: %s", token.value)
	}
	field := p.fieldValue(token.value)
	p.index++
	operator := p.peek().value
	if operator == "." {
		p.index++
		function := p.peek()
		if function.kind != "ident" {
			return false, fmt.Errorf("expected method name")
		}
		p.index++
		if p.peek().value != "(" {
			return false, fmt.Errorf("expected '(' after %s", function.value)
		}
		p.index++
		argument, err := p.parseString()
		if err != nil {
			return false, err
		}
		if p.peek().value != ")" {
			return false, fmt.Errorf("missing closing parenthesis")
		}
		p.index++
		return applySelectorMethod(field, function.value, argument)
	}
	if operator != "==" && operator != "!=" {
		return false, fmt.Errorf("expected comparison operator, got %q", operator)
	}
	p.index++
	value, err := p.parseString()
	if err != nil {
		return false, err
	}
	if operator == "==" {
		return field == value, nil
	}
	return field != value, nil
}

func (p *selectorParser) parseString() (string, error) {
	token := p.peek()
	if token.kind != "string" {
		return "", fmt.Errorf("expected string, got %q", token.value)
	}
	p.index++
	return token.value, nil
}

func (p *selectorParser) fieldValue(field string) string {
	switch field {
	case "path":
		return p.context.Path
	case "format":
		return p.context.Format
	case "repository", "repo":
		return p.context.Repository
	default:
		return ""
	}
}

func applySelectorMethod(value, method, argument string) (bool, error) {
	switch method {
	case "startsWith":
		return strings.HasPrefix(value, argument), nil
	case "endsWith":
		return strings.HasSuffix(value, argument), nil
	case "contains":
		return strings.Contains(value, argument), nil
	case "matches":
		expression, err := regexp.Compile(argument)
		if err != nil {
			return false, err
		}
		return expression.MatchString(value), nil
	default:
		return false, fmt.Errorf("unsupported method: %s", method)
	}
}

// SelectorResolver resolves a selector name to its expression.
type SelectorResolver func(name string) (string, bool)

// CanPath reports whether the principal may perform an action on a specific
// path, considering selector-scoped privileges.
func (p Principal) CanPath(format, repository, action, path string, resolver SelectorResolver) bool {
	if p.Can(fmt.Sprintf("nexus:repository-view:%s:%s:%s", format, repository, action)) {
		return true
	}
	if resolver == nil {
		return false
	}
	for _, privilege := range p.Privileges {
		parts := strings.Split(privilege, ":")
		if len(parts) != 6 || parts[0] != "nexus" || parts[1] != "repository-content-selector" {
			continue
		}
		if !segmentMatches(parts[2], format) || !segmentMatches(parts[3], repository) || parts[5] != action {
			continue
		}
		expression, ok := resolver(parts[4])
		if !ok {
			continue
		}
		matched, err := MatchSelector(expression, SelectorContext{Path: path, Format: format, Repository: repository})
		if err == nil && matched {
			return true
		}
	}
	return false
}

func segmentMatches(pattern, value string) bool {
	return pattern == "*" || pattern == value
}
