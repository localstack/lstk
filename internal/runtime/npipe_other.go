//go:build !windows

package runtime

// probeNamedPipe always reports unreachable on non-Windows platforms. Windows
// named pipes only exist on Windows, and resolveDockerContextHost only takes
// the npipe branch when stdruntime.GOOS == "windows", so this stub is never
// actually called in practice here — it exists solely so this package still
// builds when compiled for other OSes.
func probeNamedPipe(_ string) bool {
	return false
}
