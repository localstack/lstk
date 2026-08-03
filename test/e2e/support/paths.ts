import { realpath } from "node:fs/promises";
import path from "node:path";

/**
 * The canonical form of a path, for comparing one the binary printed against one this
 * suite built.
 *
 * The two can disagree without either being wrong: macOS resolves `/var/...` to
 * `/private/var/...`, and Windows may report the 8.3 short form (`C:\Users\RUNNER~1`)
 * where Node reports the long one (`C:\Users\runneradmin`). `realpath` collapses both,
 * and the Go suite carries an equivalent helper for the same reason.
 *
 * Works on paths that do not exist yet: the deepest existing ancestor is resolved and
 * the remaining segments appended, since only the existing part can be aliased.
 */
export async function canonicalPath(target: string): Promise<string> {
  const absolute = path.resolve(target);
  const trailing: string[] = [];
  let current = absolute;

  for (;;) {
    try {
      const resolved = await realpath(current);
      return trailing.length > 0 ? path.join(resolved, ...trailing.reverse()) : resolved;
    } catch {
      const parent = path.dirname(current);
      if (parent === current) return absolute; // nothing along the path exists
      trailing.push(path.basename(current));
      current = parent;
    }
  }
}
