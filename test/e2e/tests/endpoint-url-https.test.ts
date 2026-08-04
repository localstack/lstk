import { execa } from "execa";
import { describe, expect, test } from "vitest";
import { lstk, requirement, tempHome, unreachableDockerHost } from "../support/index.ts";
import { emulatorApi, sslCertFileTrusted, stateExportZip } from "../support/emulator-api.ts";
import { fakeBinary } from "../support/fake-binary.ts";

// Ported from test/integration/endpoint_url_https_test.go.
//
// A LocalStack cloud-hosted ephemeral instance is reached over https, so the
// resolved scheme has to survive all the way to whatever finally makes the
// request — the wrapped aws/cdk tools and the emulator API calls behind
// snapshot/status — instead of being reconstructed as "http://"+host.
//
// These need lstk to trust a throwaway certificate, and lstk has no --insecure
// flag by design. The only handle is the OS trust mechanism its TLS stack reads,
// and Go's crypto/x509 honours SSL_CERT_FILE on Linux only: darwin goes through
// Security.framework and Windows through its own store. So all but the last test
// here are Linux-only, exactly as they were in Go.

const noOpenssl = requirement(
  "the openssl CLI",
  await execa("openssl", ["version"], { reject: false }).then((r) => r.exitCode === 0),
  "Install openssl, used to mint a throwaway certificate for the https mock.",
);
const noCertTrust = requirement(
  "SSL_CERT_FILE certificate trust",
  sslCertFileTrusted,
  "Run on Linux: Go's x509 verifier only reads SSL_CERT_FILE there, so an exec'd lstk cannot be told to trust the test certificate on macOS or Windows.",
);

/** Env that both breaks Docker and makes lstk trust the mock's certificate. */
function tlsEnv(certFile: string): Record<string, string> {
  return { DOCKER_HOST: unreachableDockerHost, SSL_CERT_FILE: certFile };
}

describe.skipIf(noOpenssl || noCertTrust)("an https endpoint", () => {
  test("reaches the wrapped aws CLI with the https scheme intact", async () => {
    const api = await emulatorApi({ version: "3.0.2", tls: true });
    const aws = await fakeBinary({ name: "aws" });
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "aws", "s3", "ls"], {
      home,
      env: { ...tlsEnv(api.certFile!), PATH: aws.path },
    });

    expect(run).toSucceed();
    expect((await aws.lastCall())?.args).toEqual(["--endpoint-url", api.url, "s3", "ls"]);
  });

  test("reaches the wrapped cdk with the https scheme intact", async () => {
    const api = await emulatorApi({ version: "3.0.2", tls: true });
    const cdk = await fakeBinary({
      name: "cdk",
      responses: [{ when: ["--version"], stdout: "2.177.0\n" }],
    });
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "cdk", "deploy", "MyStack"], {
      home,
      env: { ...tlsEnv(api.certFile!), PATH: cdk.path },
    });

    expect(run).toSucceed();
    expect((await cdk.lastCall())?.env.AWS_ENDPOINT_URL).toBe(api.url);
  });

  // The emulator-client layer, rather than a wrapped tool: `snapshot save` calls
  // /_localstack/pods/state itself.
  test("is where snapshot save exports state from", async () => {
    const api = await emulatorApi({ version: "3.0.2", tls: true, stateExport: stateExportZip() });
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "snapshot", "save"], {
      home,
      env: tlsEnv(api.certFile!),
      cwd: home.path,
    });

    expect(run).toSucceed();
    expect(run).toPrint("Snapshot saved");
    expect(api.requestedPaths()).toContain("/_localstack/pods/state");
  });

  test("renders reachability, type and version, with no container fields", async () => {
    const api = await emulatorApi({ version: "3.0.2", services: { s3: "available" }, tls: true });
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", api.url, "status"], {
      home,
      env: tlsEnv(api.certFile!),
    });

    expect(run).toSucceed();
    expect(run.stdout).toPrintExactly(`
      Fetching LocalStack status...
      ✔︎ LocalStack AWS Emulator is running
      • Endpoint: ${api.url}
      • Version: 3.0.2
      > Note: No resources deployed
    `);
  });

  // The confusing case a user actually hit: nothing listens on port 80 of a
  // cloud-hosted instance, so an `http://` URL produced a bare "no route to
  // host" that never hinted the scheme was wrong.
  test("is suggested by name when the user reached for http", async () => {
    const api = await emulatorApi({ version: "3.0.2", tls: true });
    const httpUrl = api.url.replace("https://", "http://");
    const home = await tempHome();

    const run = await lstk(["--endpoint-url", httpUrl, "status"], {
      home,
      env: tlsEnv(api.certFile!),
    });

    expect(run).toExitWith(1);
    expect(run).toPrint(`could not reach LocalStack emulator at ${httpUrl}`);
    expect(run).toPrint(`${api.url} responded`);
    expect(run).toPrint("retry with that URL");
    expect(run, "the user manages this endpoint themselves").not.toPrint("lstk start");
  });
});
