import { execa } from "execa";
import http from "node:http";
import https from "node:https";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import zlib from "node:zlib";
import { onTestFinished } from "vitest";

/**
 * A stand-in for the emulator's own HTTP API — the endpoints lstk calls once it
 * has decided *where* the emulator is: `/_localstack/health`, `/_localstack/info`,
 * `/_localstack/resources` and `/_localstack/pods/state`.
 *
 * This is what `--endpoint-url` points at. Unlike `emulator-stub.ts` (a container
 * that merely exists, so discovery finds something) nothing here runs in Docker:
 * an externally-managed endpoint is by definition one lstk did not start, so these
 * tests pair the server with an unreachable DOCKER_HOST to prove Docker is never
 * consulted.
 *
 * Mirrors `awsHealthHandler` / `awsHealthServer` in
 * test/integration/endpoint_url_test.go.
 */
export interface EmulatorApiOptions {
  /**
   * Version reported by `/_localstack/health`. Omit to leave the key out
   * entirely, which is how lstk tells an Azure emulator apart from an AWS one —
   * it falls back to `/_localstack/info` for the version.
   */
  version?: string;
  edition?: string;
  services?: Record<string, string>;
  /** Body for `/_localstack/resources` (NDJSON). Defaults to an empty listing. */
  resources?: string;
  /** Response for `/_localstack/info`, served only when set. */
  info?: Record<string, unknown>;
  /** Serve `/_localstack/pods/state` with this body, for `snapshot save`. */
  stateExport?: Buffer;
  /** Serve over TLS with a throwaway self-signed certificate for 127.0.0.1. */
  tls?: boolean;
}

export interface EmulatorApi {
  /** Base URL, e.g. `http://127.0.0.1:54321`. */
  readonly url: string;
  /** `host:port`, for `LOCALSTACK_HOST`. */
  readonly hostPort: string;
  /**
   * PEM file holding the server's certificate, for `tls: true` only.
   *
   * lstk has no `--insecure` flag by design, so a subprocess can only trust a
   * throwaway cert through the OS trust mechanism its TLS stack reads. Go's
   * crypto/x509 honours SSL_CERT_FILE only on the unix builds listed in
   * root_unix.go — not darwin (Security.framework) and not Windows. Hence
   * `sslCertFileTrustUnavailable` below.
   */
  readonly certFile?: string;
  /** Paths requested so far, oldest first. */
  requestedPaths(): string[];
}

/**
 * Whether this platform's Go TLS verifier honours SSL_CERT_FILE, which is the
 * only handle a test has on what an exec'd lstk trusts.
 */
export const sslCertFileTrusted = process.platform === "linux";

async function selfSignedCert(): Promise<{ key: string; cert: string; certFile: string }> {
  const dir = await mkdtemp(path.join(os.tmpdir(), "lstk-e2e-tls-"));
  const keyFile = path.join(dir, "key.pem");
  const certFile = path.join(dir, "cert.pem");

  // openssl rather than a committed fixture: a checked-in private key trips the
  // repo's gitleaks pre-commit hook, and a checked-in cert eventually expires.
  await execa("openssl", [
    "req", "-x509",
    "-newkey", "rsa:2048",
    "-keyout", keyFile,
    "-out", certFile,
    "-days", "1",
    "-nodes",
    "-subj", "/CN=127.0.0.1",
    "-addext", "subjectAltName=IP:127.0.0.1",
  ]);

  return {
    key: await readFile(keyFile, "utf8"),
    cert: await readFile(certFile, "utf8"),
    certFile,
  };
}

export async function emulatorApi(options: EmulatorApiOptions = {}): Promise<EmulatorApi> {
  const paths: string[] = [];

  const handler = (req: http.IncomingMessage, res: http.ServerResponse): void => {
    const url = new URL(req.url ?? "/", "http://localhost");
    paths.push(url.pathname);

    switch (url.pathname) {
      case "/_localstack/health": {
        const body: Record<string, unknown> = {
          services: options.services ?? { s3: "available", sqs: "available" },
        };
        if (options.version !== undefined) body.version = options.version;
        if (options.edition !== undefined) body.edition = options.edition;
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify(body));
        return;
      }
      case "/_localstack/info": {
        if (options.info === undefined) break;
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify(options.info));
        return;
      }
      case "/_localstack/resources": {
        res.writeHead(200, { "Content-Type": "application/x-ndjson" });
        res.end(options.resources ?? "");
        return;
      }
      case "/_localstack/pods/state": {
        if (options.stateExport === undefined) break;
        res.writeHead(200, { "Content-Type": "application/zip" });
        res.end(options.stateExport);
        return;
      }
      default:
        break;
    }
    res.writeHead(404).end();
  };

  let certFile: string | undefined;
  let server: http.Server | https.Server;
  if (options.tls) {
    const { key, cert, certFile: file } = await selfSignedCert();
    certFile = file;
    server = https.createServer({ key, cert }, handler);
  } else {
    server = http.createServer(handler);
  }

  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  onTestFinished(() => new Promise<void>((resolve) => server.close(() => resolve())));

  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("emulator API mock did not bind to a TCP port");
  }
  const hostPort = `127.0.0.1:${address.port}`;

  return {
    url: `${options.tls ? "https" : "http"}://${hostPort}`,
    hostPort,
    ...(certFile === undefined ? {} : { certFile }),
    requestedPaths: () => [...paths],
  };
}

/**
 * A "server that is reachable but is not LocalStack" — 404 on everything, so type
 * detection cannot conclude anything and lstk must fail closed.
 */
export async function notLocalStackServer(): Promise<{ url: string }> {
  const server = http.createServer((_req, res) => res.writeHead(404).end());
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  onTestFinished(() => new Promise<void>((resolve) => server.close(() => resolve())));

  const address = server.address();
  if (address === null || typeof address === "string") {
    throw new Error("mock server did not bind to a TCP port");
  }
  return { url: `http://127.0.0.1:${address.port}` };
}

/**
 * A minimal store-only ZIP, the shape `/_localstack/pods/state` returns.
 *
 * `snapshot save` streams the response body straight to disk without parsing it,
 * so the bytes only have to be a plausible export — but a real archive keeps the
 * fixture honest if that ever changes. Mirrors `minimalStateZip` in
 * test/integration/endpoint_url_https_test.go.
 */
export function stateExportZip(name = "state.json", content = `{"services":{}}`): Buffer {
  const nameBytes = Buffer.from(name, "utf8");
  const data = Buffer.from(content, "utf8");
  const crc = zlib.crc32(data);

  const localHeader = Buffer.alloc(30);
  localHeader.writeUInt32LE(0x04034b50, 0); // local file header signature
  localHeader.writeUInt16LE(20, 4); // version needed
  localHeader.writeUInt16LE(0, 8); // method: store
  localHeader.writeUInt32LE(crc, 14);
  localHeader.writeUInt32LE(data.length, 18);
  localHeader.writeUInt32LE(data.length, 22);
  localHeader.writeUInt16LE(nameBytes.length, 26);

  const centralHeader = Buffer.alloc(46);
  centralHeader.writeUInt32LE(0x02014b50, 0); // central directory header signature
  centralHeader.writeUInt16LE(20, 4); // version made by
  centralHeader.writeUInt16LE(20, 6); // version needed
  centralHeader.writeUInt16LE(0, 10); // method: store
  centralHeader.writeUInt32LE(crc, 16);
  centralHeader.writeUInt32LE(data.length, 20);
  centralHeader.writeUInt32LE(data.length, 24);
  centralHeader.writeUInt16LE(nameBytes.length, 28);

  const centralOffset = localHeader.length + nameBytes.length + data.length;
  const centralSize = centralHeader.length + nameBytes.length;

  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0); // end of central directory signature
  end.writeUInt16LE(1, 8); // entries on this disk
  end.writeUInt16LE(1, 10); // total entries
  end.writeUInt32LE(centralSize, 12);
  end.writeUInt32LE(centralOffset, 16);

  return Buffer.concat([localHeader, nameBytes, data, centralHeader, nameBytes, end]);
}
