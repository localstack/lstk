import path from "node:path";

/**
 * Mirrors internal/config/paths.go's osConfigDir(): the lowest-priority tier
 * of lstk's config search order, which is Go's os.UserConfigDir() for a given
 * HOME. Needed so tests can predict where lstk will look without importing
 * lstk source — same role as the Go integration suite's own
 * expectedOSConfigDir helper (test/integration/config_test.go).
 *
 * os.UserConfigDir() honors $XDG_CONFIG_HOME on Linux but ignores it on macOS
 * and Windows (those use fixed native paths), so xdgConfigHome only matters
 * for the Linux branch below.
 */
export function osConfigDir(home: string, xdgConfigHome?: string): string {
  switch (process.platform) {
    case "darwin":
      return path.join(home, "Library", "Application Support", "lstk");
    case "win32":
      return path.join(home, "AppData", "Roaming", "lstk");
    default:
      if (xdgConfigHome) return path.join(xdgConfigHome, "lstk");
      return path.join(home, ".config", "lstk");
  }
}

/**
 * lstk's tier-2 config directory: always $HOME/.config/lstk, on every
 * platform including Windows — internal/config/paths.go's xdgConfigDir() does
 * not consult $XDG_CONFIG_HOME at all, unlike osConfigDir() above.
 */
export function xdgConfigDir(home: string): string {
  return path.join(home, ".config", "lstk");
}
