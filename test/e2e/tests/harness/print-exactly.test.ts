import { describe, expect, test } from "vitest";

// Guards toPrintExactly's dedent. Every output assertion in the suite runs through
// it, so a helper that silently mangles the expected text would quietly weaken all
// of them at once.

describe("toPrintExactly", () => {
  test("drops surrounding blank lines and the common indent", () => {
    expect("a\nb").toPrintExactly(`
      a
      b
    `);
  });

  test("preserves nesting inside the block", () => {
    expect("a\n  b").toPrintExactly(`
      a
        b
    `);
  });

  test("matches an empty stream", () => {
    expect("").toPrintExactly("");
  });

  test("keeps blank lines in the middle", () => {
    expect("a\n\nb").toPrintExactly(`
      a

      b
    `);
  });

  test("accepts a single-line literal with no block form", () => {
    expect("just one line").toPrintExactly("just one line");
  });

  test("fails when the text differs", () => {
    expect(() => expect("a").toPrintExactly("b")).toThrow();
  });

  test("fails on trailing whitespace the CLI did not print", () => {
    expect(() => expect("a").toPrintExactly("a ")).toThrow();
  });
});
