import type { Home } from "./home.ts";

/**
 * Makes CLI output stable enough to inline-snapshot by masking the parts that
 * differ per machine or per run.
 *
 * Only reach for this when the varying part is incidental to what the test is
 * about. If the value itself is the point — a specific port, a specific resolved
 * config path — assert it explicitly instead of masking it away.
 */
export interface NormalizeOptions {
  /** Replaces this home's temp directory with `<home>`. */
  home?: Home;
  /** Replaces each `[find, replace]` pair, applied after the built-in masks. */
  extra?: Array<[RegExp | string, string]>;
}

export function normalizeCliOutput(text: string, options: NormalizeOptions = {}): string {
  let out = text;

  if (options.home) {
    // macOS reports /private/var/... where the env said /var/..., so mask both
    // forms — longest first, or masking the short form would leave a stray
    // "/private" in front of the placeholder.
    const home = options.home.path;
    const variants = [`/private${home}`, home, home.replace(/^\/private/, "")];
    for (const variant of [...new Set(variants)].sort((a, b) => b.length - a.length)) {
      out = out.split(variant).join("<home>");
    }
  }

  for (const [find, replace] of options.extra ?? []) {
    out = typeof find === "string" ? out.split(find).join(replace) : out.replace(find, replace);
  }

  return out;
}
