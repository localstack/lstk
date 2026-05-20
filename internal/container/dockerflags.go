package container

import (
	"fmt"
	"strings"

	"github.com/localstack/lstk/internal/runtime"
)

// ParsedFlags holds the result of parsing a DOCKER_FLAGS-style string.
type ParsedFlags struct {
	Env   []string
	Binds []runtime.BindMount
}

// ParseDockerFlags parses a subset of docker run flags from a raw string.
// Supported: -e/--env, -v/--volume.
func ParseDockerFlags(flags string) (ParsedFlags, error) {
	tokens, err := shellTokenize(flags)
	if err != nil {
		return ParsedFlags{}, err
	}

	var result ParsedFlags
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		next := func() (string, error) {
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("flag %q requires a value", tok)
			}
			i++
			return tokens[i], nil
		}

		switch {
		case tok == "-e" || tok == "--env":
			val, err := next()
			if err != nil {
				return ParsedFlags{}, err
			}
			result.Env = append(result.Env, val)
		case strings.HasPrefix(tok, "-e") && len(tok) > 2:
			result.Env = append(result.Env, tok[2:])
		case strings.HasPrefix(tok, "--env="):
			result.Env = append(result.Env, strings.TrimPrefix(tok, "--env="))

		case tok == "-v" || tok == "--volume":
			val, err := next()
			if err != nil {
				return ParsedFlags{}, err
			}
			b, err := parseBindMount(val)
			if err != nil {
				return ParsedFlags{}, err
			}
			result.Binds = append(result.Binds, b)
		case strings.HasPrefix(tok, "-v") && len(tok) > 2:
			b, err := parseBindMount(tok[2:])
			if err != nil {
				return ParsedFlags{}, err
			}
			result.Binds = append(result.Binds, b)
		case strings.HasPrefix(tok, "--volume="):
			b, err := parseBindMount(strings.TrimPrefix(tok, "--volume="))
			if err != nil {
				return ParsedFlags{}, err
			}
			result.Binds = append(result.Binds, b)

		default:
			return ParsedFlags{}, fmt.Errorf("unsupported docker flag: %q", tok)
		}
	}
	return result, nil
}

// parseBindMount parses a HOST:CONTAINER[:ro] volume spec into a BindMount.
func parseBindMount(spec string) (runtime.BindMount, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 {
		return runtime.BindMount{}, fmt.Errorf("invalid volume spec %q: must be HOST:CONTAINER[:ro]", spec)
	}
	b := runtime.BindMount{HostPath: parts[0], ContainerPath: parts[1]}
	if len(parts) == 3 {
		b.ReadOnly = parts[2] == "ro"
	}
	return b, nil
}

// shellTokenize splits s into tokens using shell-like whitespace splitting,
// respecting single and double quotes.
func shellTokenize(s string) ([]string, error) {
	var tokens []string
	var cur strings.Builder
	inQuote := rune(0)

	for _, c := range s {
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == ' ' || c == '\t':
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(c)
		}
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("unterminated quote in docker flags")
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens, nil
}
