package awscli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCompLineAlwaysStartsWithAWS(t *testing.T) {
	// The completer drops the first word and resolves the rest against the aws
	// command tree, so lstk's own name must never appear in the line.
	assert.Equal(t, "aws s3 l", buildCompLine([]string{"s3"}, "l"))
	assert.Equal(t, "aws ", buildCompLine(nil, ""))
	assert.Equal(t, "aws s3api list-buckets --pre", buildCompLine([]string{"s3api", "list-buckets"}, "--pre"))
}

func TestBuildCompLineKeepsTrailingSpaceForNewWord(t *testing.T) {
	// An empty toComplete means the cursor sits after a space; without the
	// trailing space the completer would try to complete the previous word.
	assert.Equal(t, "aws s3 ", buildCompLine([]string{"s3"}, ""))
}

func TestParseCompleterOutput(t *testing.T) {
	assert.Equal(t, []string{"ls", "cp", "mv"}, parseCompleterOutput("ls\ncp\nmv\n"))
	assert.Empty(t, parseCompleterOutput(""))
	assert.Empty(t, parseCompleterOutput("\n\n"))
	assert.Equal(t, []string{"ls"}, parseCompleterOutput("  ls  \n\n"))
	// A tab would be read by Cobra as the candidate/description separator.
	assert.Equal(t, []string{"ls"}, parseCompleterOutput("ls\ncp\tdescription\n"))
}

// writeFakeCompleter writes an executable named name into dir that echoes back
// the completion protocol's inputs, one per line.
func writeFakeCompleter(t *testing.T, dir, name string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake completer script not supported on Windows")
	}
	script := "#!/bin/sh\necho \"LINE:$COMP_LINE\"\necho \"POINT:$COMP_POINT\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
}

func TestCompleterPathFindsBinaryOnPATH(t *testing.T) {
	dir := t.TempDir()
	writeFakeCompleter(t, dir, completerBinary)
	t.Setenv("PATH", dir)

	path, err := CompleterPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, completerBinary), path)
}

// TestCompleterPathFallsBackToAWSSibling covers installs that expose only aws
// on PATH: the v2 installer symlinks /usr/local/bin/aws into an install dir
// that also holds aws_completer.
func TestCompleterPathFallsBackToAWSSibling(t *testing.T) {
	installDir := t.TempDir()
	writeFakeCompleter(t, installDir, completerBinary)
	require.NoError(t, os.WriteFile(filepath.Join(installDir, "aws"), []byte("#!/bin/sh\n"), 0o755))

	binDir := t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(installDir, "aws"), filepath.Join(binDir, "aws")))
	t.Setenv("PATH", binDir)

	path, err := CompleterPath()
	require.NoError(t, err)

	// t.TempDir can hand back a symlinked path (/var -> /private/var on macOS),
	// so compare the resolved locations.
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(filepath.Join(installDir, completerBinary))
	require.NoError(t, err)
	assert.Equal(t, want, resolved)
}

func TestCompleterPathErrsWhenAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := CompleterPath()
	assert.ErrorIs(t, err, ErrCompleterNotFound)
}

func TestCompleteFeedsCompletionProtocolToCompleter(t *testing.T) {
	dir := t.TempDir()
	writeFakeCompleter(t, dir, completerBinary)
	t.Setenv("PATH", dir)

	candidates, err := Complete(context.Background(), []string{"s3"}, "l")
	require.NoError(t, err)
	assert.Equal(t, []string{"LINE:aws s3 l", "POINT:8"}, candidates)
}

func TestCompleteErrsWhenCompleterAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := Complete(context.Background(), []string{"s3"}, "l")
	assert.ErrorIs(t, err, ErrCompleterNotFound)
}

// TestCompleteHonorsContextDeadline covers the caller-owned bound on how long
// a Tab press may block: Complete sets no deadline of its own, so a wedged
// completer must be killed by the context the command boundary supplies.
func TestCompleteHonorsContextDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake completer script not supported on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, completerBinary),
		[]byte("#!/bin/sh\nsleep 30\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Complete(ctx, nil, "")
	assert.Error(t, err)
	assert.Less(t, time.Since(start), 25*time.Second)
}

func TestCompleteErrsWhenCompleterFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake completer script not supported on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, completerBinary),
		[]byte("#!/bin/sh\nexit 1\n"), 0o755))
	t.Setenv("PATH", dir)

	_, err := Complete(context.Background(), nil, "")
	assert.Error(t, err)
}
