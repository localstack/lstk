'use strict';

const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');
const { once } = require('node:events');

const { resolveBinaryPath } = require('./launcher');

const LAUNCHER = path.join(__dirname, 'launcher.js');

// Build a throwaway npm package layout mirroring what the publisher ships: a
// main package containing the launcher plus a platform-specific optional
// dependency that carries the (fake) binary.
function makePackage(t, { binarySource, os: pkgOs, cpu, withDep = true }) {
  // realpathSync resolves the symlinked temp dir (on macOS os.tmpdir() is
  // /var/folders/... which is a symlink to /private/var/folders/...) so it
  // matches what require.resolve returns inside resolveBinaryPath.
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'lstk-launcher-')));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));

  fs.copyFileSync(LAUNCHER, path.join(root, 'index.js'));

  const depName = 'lstk-fake-platform';
  fs.writeFileSync(
    path.join(root, 'package.json'),
    JSON.stringify({
      name: 'lstk',
      bin: { lstk: 'index.js' },
      optionalDependencies: withDep ? { [depName]: '0.0.0' } : {},
    }),
  );

  if (withDep) {
    const depDir = path.join(root, 'node_modules', depName);
    fs.mkdirSync(depDir, { recursive: true });
    fs.writeFileSync(
      path.join(depDir, 'package.json'),
      JSON.stringify({
        name: depName,
        version: '0.0.0',
        bin: { lstk: 'lstk' },
        os: [pkgOs ?? process.platform],
        cpu: [cpu ?? process.arch],
      }),
    );
    const binaryPath = path.join(depDir, 'lstk');
    fs.writeFileSync(binaryPath, binarySource);
    fs.chmodSync(binaryPath, 0o755);
  }

  return root;
}

test('resolveBinaryPath finds the matching platform binary', (t) => {
  const root = makePackage(t, { binarySource: '#!/bin/sh\nexit 0\n' });
  const resolved = resolveBinaryPath(root);
  assert.strictEqual(
    resolved,
    path.join(root, 'node_modules', 'lstk-fake-platform', 'lstk'),
  );
});

test('resolveBinaryPath returns null when no optional dependency matches', (t) => {
  const root = makePackage(t, {
    binarySource: '#!/bin/sh\nexit 0\n',
    os: 'nonexistent-os',
  });
  assert.strictEqual(resolveBinaryPath(root), null);
});

// The regression test for DEVX-942: a signal sent to the wrapper must reach the
// child, and the child's exit status must propagate back out.
test('forwards SIGTERM to the child and propagates its exit code', { skip: process.platform === 'win32' }, async (t) => {
  const flag = path.join(os.tmpdir(), `lstk-sigterm-${process.pid}-${Date.now()}`);
  t.after(() => fs.rmSync(flag, { force: true }));

  const binarySource = `#!/usr/bin/env node
process.on('SIGTERM', () => {
  require('fs').writeFileSync(${JSON.stringify(flag)}, 'SIGTERM');
  process.exit(42);
});
process.stdout.write('ready\\n');
setInterval(() => {}, 1000);
`;
  const root = makePackage(t, { binarySource });

  const child = spawn(process.execPath, [path.join(root, 'index.js')], {
    stdio: ['ignore', 'pipe', 'inherit'],
  });

  // Wait until the fake binary has installed its handler and is running.
  await new Promise((resolve) => {
    child.stdout.on('data', (chunk) => {
      if (chunk.toString().includes('ready')) resolve();
    });
  });

  child.kill('SIGTERM');
  const [code] = await once(child, 'exit');

  assert.strictEqual(code, 42, 'wrapper should exit with the child exit code');
  assert.strictEqual(fs.readFileSync(flag, 'utf8'), 'SIGTERM', 'child should receive SIGTERM');
});
