import { describe, expect, test } from "vitest";
import { stripAnsi } from "../../support/pty.ts";

// Terminal assertions are only as good as this: if a repaint's escape sequences
// survive stripping, `waitFor` misses text a human plainly sees. ConPTY (Windows)
// emits more of them than a Unix PTY, hence the coverage here.

const ESC = String.fromCharCode(27);
const BEL = String.fromCharCode(7);

describe("stripAnsi", () => {
  test.each([
    {
      what: "colour and cursor CSI sequences",
      raw: `${ESC}[?25l${ESC}[2J${ESC}[H${ESC}[1;34mWhich emulator${ESC}[0m would you like to use?`,
      want: "Which emulator would you like to use?",
    },
    {
      what: "an OSC title sequence terminated by BEL",
      raw: `${ESC}]0;lstk${BEL}plain text`,
      want: "plain text",
    },
    {
      what: "truecolor foreground plus trailing padding",
      raw: `${ESC}[38;2;94;106;210mAWS emulator selected.${ESC}[m   `,
      want: "AWS emulator selected.",
    },
    {
      what: "alt-screen switches, charset selection and a cursor query",
      raw: `${ESC}[6n${ESC}(B${ESC}[m${ESC}[?1049hbody${ESC}[?1049l`,
      want: "body",
    },
    {
      what: "CRLF line endings, as ConPTY always emits",
      raw: "first\r\nsecond\r\n",
      want: "first\nsecond\n",
    },
  ])("strips $what", ({ raw, want }) => {
    expect(stripAnsi(raw)).toBe(want);
  });
});
