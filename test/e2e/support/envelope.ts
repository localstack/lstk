/**
 * The `--json` contract: one Envelope object on stdout and nothing else.
 * See docs/structured-output.md.
 */
export interface Envelope<Data = unknown> {
  schemaVersion: number;
  command: string;
  /** The wire values are "ok" and "error" (internal/output/envelope.go). */
  status: "ok" | "error";
  data?: Data;
  warnings: Array<{ code: string; message: string }>;
  error?: {
    code: string;
    category: string;
    message: string;
    retryable: boolean;
    details?: Record<string, unknown>;
  };
}

/** Parses stdout as an envelope, failing loudly if anything else was printed. */
export function parseEnvelope<Data = unknown>(stdout: string): Envelope<Data> {
  let envelope: Envelope<Data>;
  try {
    envelope = JSON.parse(stdout) as Envelope<Data>;
  } catch {
    throw new Error(`stdout should be exactly one JSON object, got:\n${stdout}`);
  }
  if (!Array.isArray(envelope.warnings)) {
    throw new Error("warnings should always be an array, never omitted or null");
  }
  return envelope;
}
