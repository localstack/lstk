package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/localstack/lstk/internal/output"
)

// installDirWritable reports whether the directory holding the given
// executable path can be written to, which is what an in-place binary update
// requires. It is the backstop for install methods no path marker in
// externalMarkers recognizes — a root-owned /usr/bin install run as a normal
// user, a read-only container layer, an immutable store lstk has not been
// taught about.
//
// It probes by creating and removing a file rather than calling access(2),
// which can report success for root or under an ACL that the subsequent rename
// would still fail. The probe costs ~50µs against access(2)'s ~5µs, which is
// why it is confined to the explicit `lstk update` path — where it precedes a
// multi-megabyte download and the difference is noise. It must never be put on
// the automatic start-path check, which runs on every `lstk start`.
//
// A permission error means "not writable" rather than a failure; any other
// error is returned, so a caller never reads an unrelated I/O fault as a
// read-only install.
func installDirWritable(exePath string) (bool, error) {
	dir := filepath.Dir(exePath)
	f, err := os.CreateTemp(dir, ".lstk-update-probe-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, os.ErrPermission) {
			return false, nil
		}
		// A read-only filesystem surfaces as EROFS, which is not ErrPermission.
		if isReadOnlyFSError(err) {
			return false, nil
		}
		return false, fmt.Errorf("cannot determine whether %s is writable: %w", dir, err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return false, fmt.Errorf("cannot close write probe in %s: %w", dir, err)
	}
	if err := os.Remove(name); err != nil {
		return false, fmt.Errorf("cannot remove write probe %s: %w", name, err)
	}
	return true, nil
}

// isReadOnlyFSError reports whether err is a read-only filesystem error, which
// is how an immutable store (nix, a read-only container layer) refuses a write
// rather than with a permission error. syscall.EROFS is defined on Windows too,
// so this needs no per-platform variant.
func isReadOnlyFSError(err error) bool {
	return errors.Is(err, syscall.EROFS)
}

// selfUpdateBlocker explains why lstk must not replace its own binary in place.
// Manager names the external tool that owns the install; it is empty when the
// only problem is that the install directory cannot be written to.
type selfUpdateBlocker struct {
	Manager string
	Path    string
}

func (b selfUpdateBlocker) title() string {
	if b.Manager != "" {
		return fmt.Sprintf("lstk is managed by %s and will not update itself", b.Manager)
	}
	return "lstk cannot update itself: its install directory is not writable"
}

func (b selfUpdateBlocker) summary() string {
	return fmt.Sprintf("Installed at %s", b.Path)
}

func (b selfUpdateBlocker) action() output.ErrorAction {
	if b.Manager != "" {
		return output.ErrorAction{
			Label: fmt.Sprintf("Update it through %s, or force an in-place replacement:", b.Manager),
			Value: "lstk update --force",
		}
	}
	return output.ErrorAction{
		Label: fmt.Sprintf("Update lstk the way it was installed, grant write access to %s, or force it:", filepath.Dir(b.Path)),
		Value: "lstk update --force",
	}
}

// blockSelfUpdate reports why an in-place binary replacement must not be
// attempted, or nil when it may proceed.
//
// Homebrew and npm installs are never blocked: they delegate to `brew upgrade`
// and `npm install -g`, which own the install directory themselves and work
// even where lstk cannot write to it directly.
//
// An indeterminate writability probe deliberately does not block. Guessing
// "read-only" from an unrelated I/O error would refuse an update that would
// have worked; falling through instead leaves the pre-existing behavior, where
// the rename reports the real failure.
func blockSelfUpdate(info InstallInfo) *selfUpdateBlocker {
	if info.Method == InstallExternal {
		return &selfUpdateBlocker{Manager: info.Manager, Path: info.ResolvedPath}
	}
	if info.Method != InstallBinary {
		return nil
	}
	// os.Executable() failed, so there is no install directory to probe.
	// filepath.Dir("") is ".", which would write the probe into the user's
	// working directory and report an unrelated path in the refusal.
	if info.ResolvedPath == "" {
		return nil
	}
	writable, err := installDirWritable(info.ResolvedPath)
	if err != nil || writable {
		return nil
	}
	return &selfUpdateBlocker{Path: info.ResolvedPath}
}
