package extension

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"sort"
	"strings"

	"github.com/localstack/lstk/internal/log"
)

// ErrNotFound is returned by Resolve when no matching extension executable
// exists in the bundled directory or on PATH.
var ErrNotFound = errors.New("extension not found")

// Resolver discovers and resolves extension executables. It searches the
// bundled-extensions directory (BundledDir) before PATH, so a bundled extension
// wins over a same-named executable on PATH. A zero BundledDir disables the
// bundled search (used in tests that exercise only the PATH path).
type Resolver struct {
	BundledDir string
	logger     log.Logger
}

// NewResolver returns a Resolver whose bundled-extensions directory is derived
// from the symlink-resolved location of the running lstk executable, so it is
// found even when lstk is invoked through a symlink or package shim.
func NewResolver(logger log.Logger) *Resolver {
	return &Resolver{
		BundledDir: BundledDir(logger),
		logger:     logger,
	}
}

// BundledDir returns the directory in which lstk looks for bundled extensions:
// the directory containing the symlink-resolved lstk executable. Resolving
// symlinks is what makes this work through npm `.bin` links and Homebrew shims,
// where the invoked `lstk` is a link to the real binary living next to its
// bundled siblings. It returns "" when the executable path cannot be resolved.
func BundledDir(logger log.Logger) string {
	exe, err := os.Executable()
	if err != nil {
		logger.Info("extension: cannot determine executable path: %v", err)
		return ""
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		logger.Info("extension: cannot resolve executable symlinks: %v", err)
		resolved = exe
	}
	return filepath.Dir(resolved)
}

// Resolve returns the extension for the given command name, searching in
// order: the multi-call bundle in the bundled directory (for the commands its
// descriptions file lists), standalone lstk-<name> files in the bundled
// directory, then PATH. It returns ErrNotFound when no match exists anywhere.
//
// A bundle whose descriptions file cannot be loaded does not block extensions
// found elsewhere, but when nothing else provides the name that load error is
// returned instead of ErrNotFound: the user has the binary installed and asked
// for a command it very likely provides, and "unknown command" would hide the
// broken install from them.
func (r *Resolver) Resolve(name string) (*Extension, error) {
	base := NamePrefix + name

	var bundleErr error
	if r.BundledDir != "" {
		bundle, err := LoadBundle(r.BundledDir, r.logger)
		switch {
		case err != nil:
			bundleErr = err
		case bundle != nil && bundle.Provides(name):
			return &Extension{Name: name, Path: bundle.Path, Bundled: true, Argv0: base}, nil
		}
		if path := findExecutable(r.BundledDir, base); path != "" {
			return NewExtension(name, path, true), nil
		}
	}

	if path, err := exec.LookPath(base); err == nil {
		return NewExtension(name, path, false), nil
	}

	if bundleErr != nil {
		return nil, bundleErr
	}
	return nil, ErrNotFound
}

// List returns the extensions resolvable from the bundle, the bundled directory
// and PATH, de-duplicated by command name with that same precedence (so a
// bundled extension shadows a same-named PATH executable), sorted by command
// name. It never executes an extension. A bundle that fails to load is logged
// and skipped rather than failing the listing, so help rendering never breaks
// on account of it; Resolve is where that failure is reported.
//
// Bundled entries carry their help Description. With a bundle installed the
// descriptions come from the load that already parsed the file; without one
// (or with a broken one) the lenient LoadDescriptions serves the standalone
// lstk-<name> files in the bundled dir, the pre-bundling shape. PATH entries
// are always name-only, whatever the file says.
func (r *Resolver) List() []Extension {
	seen := map[string]struct{}{}
	var found []Extension

	describe := func(string) string { return "" }
	add := func(dir string, bundled bool) {
		for _, name := range scanDir(dir) {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			path := findExecutable(dir, NamePrefix+name)
			ext := NewExtension(name, path, bundled)
			if bundled {
				ext.Description = describe(name)
			}
			found = append(found, *ext)
		}
	}

	if r.BundledDir != "" {
		bundle, err := LoadBundle(r.BundledDir, r.logger)
		if err != nil {
			r.logger.Info("extension: skipping bundle in help listing: %v", err)
		}
		if bundle != nil {
			describe = bundle.Description
			for _, name := range bundle.Names() {
				seen[name] = struct{}{}
				found = append(found, Extension{
					Name:        name,
					Path:        bundle.Path,
					Bundled:     true,
					Argv0:       NamePrefix + name,
					Description: bundle.Description(name),
				})
			}
		} else {
			descriptions := LoadDescriptions(r.BundledDir, r.logger)
			describe = func(name string) string { return descriptions[name] }
		}
		add(r.BundledDir, true)
	}
	for _, dir := range pathDirs() {
		if dir == "" || dir == r.BundledDir {
			continue
		}
		add(dir, false)
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return found
}

// scanDir returns the extension command names (the part after the "lstk-"
// prefix, with any platform executable extension stripped) of the executable
// "lstk-*" files in dir. Non-executable files and subdirectories are ignored.
func scanDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		fileName := entry.Name()
		if !strings.HasPrefix(fileName, NamePrefix) {
			continue
		}
		if entry.IsDir() {
			continue
		}
		if !isExecutableFile(filepath.Join(dir, fileName)) {
			continue
		}
		name := strings.TrimPrefix(fileName, NamePrefix)
		if goruntime.GOOS == "windows" {
			// isExecutableFile accepts any regular file on Windows, so
			// executability must be decided here by PATHEXT — otherwise a
			// data file like lstk-extensions.toml lists as a phantom
			// "extensions" extension.
			ext := strings.ToLower(filepath.Ext(name))
			if !slices.Contains(windowsExts(), ext) {
				continue
			}
			name = strings.TrimSuffix(name, ext)
		}
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// findExecutable returns the path to an executable named base in dir, honoring
// platform executable extensions on Windows (PATHEXT), or "" if none is found.
func findExecutable(dir, base string) string {
	if goruntime.GOOS == "windows" {
		for _, ext := range windowsExts() {
			path := filepath.Join(dir, base+ext)
			if isExecutableFile(path) {
				return path
			}
		}
		return ""
	}
	path := filepath.Join(dir, base)
	if isExecutableFile(path) {
		return path
	}
	return ""
}

// isExecutableFile reports whether path is a regular file that is executable. On
// Windows, executability is determined by extension (handled by the caller), so
// any regular file here is considered executable; on Unix it must have an
// execute bit set.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return false
	}
	if goruntime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0111 != 0
}

// windowsExts returns the executable extensions to try on Windows, derived from
// PATHEXT with a conventional default, lower-cased and dot-prefixed.
func windowsExts() []string {
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		raw = ".COM;.EXE;.BAT;.CMD"
	}
	var exts []string
	for _, ext := range strings.Split(raw, string(os.PathListSeparator)) {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		exts = append(exts, ext)
	}
	return exts
}

func pathDirs() []string {
	return filepath.SplitList(os.Getenv("PATH"))
}
