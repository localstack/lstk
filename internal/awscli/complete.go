package awscli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// completerBinary is the shell-completion helper the AWS CLI installs next to
// the aws binary. Both v1 and v2 ship it, and both speak the same protocol as
// bash's `complete -C`: read the command line from COMP_LINE/COMP_POINT, print
// candidates to stdout one per line.
const completerBinary = "aws_completer"

var ErrCompleterNotFound = errors.New("aws_completer not found")

// CompleterPath resolves the aws_completer binary. It is usually on PATH
// alongside aws, but some installs expose only aws, so fall back to a sibling
// of the (symlink-resolved) aws binary: the v2 installer puts /usr/local/bin/aws
// there as a symlink into an install dir that also holds aws_completer.
func CompleterPath() (string, error) {
	if path, err := exec.LookPath(completerBinary); err == nil {
		return path, nil
	}
	awsBin, err := exec.LookPath("aws")
	if err != nil {
		return "", ErrCompleterNotFound
	}
	if resolved, err := filepath.EvalSymlinks(awsBin); err == nil {
		awsBin = resolved
	}
	// LookPath on a path containing a separator checks that file directly, and
	// on Windows still applies PATHEXT, so this covers aws_completer.exe too.
	sibling, err := exec.LookPath(filepath.Join(filepath.Dir(awsBin), completerBinary))
	if err != nil {
		return "", ErrCompleterNotFound
	}
	return sibling, nil
}

// Complete returns the aws CLI's own completion candidates for
// `aws <args...> <toComplete>`, where toComplete is the partial word under the
// cursor (empty when the cursor sits after a space).
//
// Bounding how long a Tab press may block is the caller's call: pass a ctx with
// a deadline.
func Complete(ctx context.Context, args []string, toComplete string) ([]string, error) {
	completer, err := CompleterPath()
	if err != nil {
		return nil, err
	}

	line := buildCompLine(args, toComplete)

	cmd := exec.CommandContext(ctx, completer)
	cmd.Env = append(os.Environ(),
		"COMP_LINE="+line,
		// COMP_POINT is a character offset into COMP_LINE (the completer slices
		// the line with it), not a byte offset.
		"COMP_POINT="+strconv.Itoa(utf8.RuneCountInString(line)),
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseCompleterOutput(string(out)), nil
}

// buildCompLine renders the command line for the completer. The first word is
// always "aws": the completer drops it and resolves the rest against the aws
// command tree, so passing lstk's own invocation ("lstk aws s3 l") yields no
// candidates at all.
//
// Words are joined with single spaces, without shell quoting. Cobra hands over
// already-split words, and the completer locates the word under the cursor by
// scanning the line itself rather than by shell-splitting it, so re-quoting a
// value that contained spaces would not restore the original word boundaries
// anyway.
func buildCompLine(args []string, toComplete string) string {
	var b strings.Builder
	b.WriteString("aws")
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	// The trailing space is significant when toComplete is empty: it is what
	// tells the completer the cursor starts a new word.
	b.WriteByte(' ')
	b.WriteString(toComplete)
	return b.String()
}

// parseCompleterOutput turns the completer's one-candidate-per-line output into
// the list Cobra expects. Cobra reads a tab in a candidate as the separator
// before its description, so candidates carrying one are dropped rather than
// silently mangled into a bogus description.
func parseCompleterOutput(out string) []string {
	var candidates []string
	for line := range strings.SplitSeq(out, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" || strings.ContainsRune(candidate, '\t') {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}
