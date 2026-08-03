import http from "node:http";
import { onTestFinished } from "vitest";

export type LicenseBehavior =
  /** Grants a license, as the platform does for a valid token. */
  | "grants"
  /** Definitively rejects (HTTP 403), as it does for an invalid token. */
  | "rejects"
  /** Returns 200 with a caller-supplied body, to exercise response parsing. */
  | { body: string };

export interface LicenseServer {
  /** Value for LSTK_API_ENDPOINT. */
  readonly url: string;
  /** Number of license requests received so far. */
  requestCount(): number;
}

/**
 * A stand-in for the LocalStack platform license API, so start-path tests never
 * depend on the real service. Shuts itself down when the test finishes.
 */
export async function mockLicenseServer(behavior: LicenseBehavior): Promise<LicenseServer> {
  let requests = 0;

  const server = http.createServer((req, res) => {
    if (req.method !== "POST" || req.url !== "/v1/license/request") {
      res.writeHead(404).end();
      return;
    }
    requests++;
    if (behavior === "rejects") {
      res.writeHead(403).end();
      return;
    }
    const body = behavior === "grants" ? `{"license_type":"ultimate"}` : behavior.body;
    res.writeHead(200, { "Content-Type": "application/json" }).end(body);
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  onTestFinished(() => new Promise<void>((resolve) => server.close(() => resolve())));

  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("mock license server did not bind to a TCP port");
  }

  return {
    url: `http://127.0.0.1:${address.port}`,
    requestCount: () => requests,
  };
}
