import http from "node:http";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { onTestFinished } from "vitest";

/**
 * A stand-in for the LocalStack platform API, covering the browser login flow
 * end to end so a test can put the binary into a logged-in state using nothing
 * but `lstk login` — no reaching into token storage from the test side.
 */
export interface MockPlatform {
  /** Value for both LSTK_API_ENDPOINT and LSTK_WEB_APP_URL. */
  readonly url: string;
  /** The license token the flow hands back, i.e. what ends up stored. */
  readonly licenseToken: string;
  /** The auth URL the binary is expected to open in a browser. */
  readonly authUrl: string;
}

export interface MockPlatformOptions {
  /** Whether the auth request reports as confirmed. Defaults to true. */
  confirmed?: boolean;
  licenseToken?: string;
}

const AUTH_REQUEST_ID = "test-auth-req-id";
const AUTH_CODE = "TEST123";

export async function mockPlatform(options: MockPlatformOptions = {}): Promise<MockPlatform> {
  const confirmed = options.confirmed ?? true;
  const licenseToken = options.licenseToken ?? "test-license-token";

  const server = http.createServer((req, res) => {
    const send = (status: number, body?: unknown) => {
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(body === undefined ? undefined : JSON.stringify(body));
    };

    const method = req.method;
    // Match on the path only: the binary sends query parameters on some of these.
    const url = new URL(req.url ?? "/", "http://localhost").pathname;
    if (method === "POST" && url === "/v1/auth/request") {
      send(201, { id: AUTH_REQUEST_ID, code: AUTH_CODE, exchange_token: "test-exchange-token" });
    } else if (method === "GET" && url === `/v1/auth/request/${AUTH_REQUEST_ID}`) {
      send(200, { confirmed });
    } else if (method === "POST" && url === `/v1/auth/request/${AUTH_REQUEST_ID}/exchange`) {
      send(200, { id: AUTH_REQUEST_ID, auth_token: "Bearer test-bearer-token" });
    } else if (method === "GET" && url === "/v1/license/credentials") {
      send(200, { token: licenseToken });
    } else if (method === "POST" && url === "/v1/license/request") {
      send(200, { license_type: "ultimate" });
    } else {
      send(404);
    }
    // Request bodies are drained by node once the response ends.
  });

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  onTestFinished(() => new Promise<void>((resolve) => server.close(() => resolve())));

  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("mock platform did not bind to a TCP port");
  }
  const url = `http://127.0.0.1:${address.port}`;

  return {
    url,
    licenseToken,
    authUrl: `${url}/auth/request/${AUTH_REQUEST_ID}?code=${AUTH_CODE}`,
  };
}

/**
 * Whether the browser can be intercepted on this platform. On Windows
 * github.com/pkg/browser invokes `rundll32 url.dll,FileProtocolHandler` rather
 * than a shimmable script, so login-flow tests cannot run there.
 */
export const browserCanBeFaked = process.platform !== "win32";

export interface FakeBrowser {
  /** PATH value to run the binary with, so no real browser tab is ever opened. */
  readonly path: string;
  /** The URL the binary asked to open, or "" if it has not asked yet. */
  openedUrl(): Promise<string>;
}

/**
 * Puts fake `open` / `xdg-open` scripts ahead of the real ones on PATH: they
 * record the URL instead of launching a browser. github.com/pkg/browser shells
 * out to whichever of these exists, so this covers macOS and Linux; on Windows it
 * calls rundll32 directly and cannot be shimmed this way.
 */
export async function fakeBrowser(): Promise<FakeBrowser> {
  const dir = await mkdtemp(path.join(os.tmpdir(), "lstk-e2e-browser-"));
  onTestFinished(async () => {
    await rm(dir, { recursive: true, force: true });
  });
  const record = path.join(dir, "opened-url");
  const script = `#!/bin/sh\nprintf '%s' "$1" > ${JSON.stringify(record)}\n`;

  await Promise.all(
    ["open", "xdg-open", "x-www-browser", "www-browser"].map((name) =>
      writeFile(path.join(dir, name), script, { mode: 0o755 }),
    ),
  );

  return {
    path: `${dir}${path.delimiter}${process.env.PATH ?? ""}`,
    async openedUrl() {
      const { readFile } = await import("node:fs/promises");
      return readFile(record, "utf8").catch(() => "");
    },
  };
}
