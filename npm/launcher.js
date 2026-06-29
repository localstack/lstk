#!/usr/bin/env node
'use strict';

// Launcher published as the `bin` of the main `@localstack/lstk` npm package.
// It locates the prebuilt Go binary shipped in the platform-specific optional
// dependency and execs it, forwarding the parent process' arguments and exit
// status.
//
// Crucially it forwards termination signals to the child. Without this a
// programmatic `kill <lstk-pid>` (e.g. from a supervisor or test harness) would
// terminate this Node wrapper but orphan the Go binary, leaving mid-flight
// container starts running. Interactive Ctrl-C already reaches the child via
// the TTY process group; the signal forwarding covers the non-interactive case.

const path = require('node:path');
const { spawn } = require('node:child_process');

const FORWARDED_SIGNALS = ['SIGINT', 'SIGTERM', 'SIGHUP'];

// Resolve the prebuilt binary from the optional dependency that npm installed
// for this host. npm only installs the optional dependency whose `os`/`cpu`
// match the current platform, so we pick the first one that both resolves and
// matches.
function resolveBinaryPath(packageDir) {
  const manifest = require(path.join(packageDir, 'package.json'));
  const deps = Object.keys(manifest.optionalDependencies || {});

  for (const dep of deps) {
    let depManifestPath;
    try {
      depManifestPath = require.resolve(path.join(dep, 'package.json'), {
        paths: [packageDir],
      });
    } catch {
      continue; // optional dependency for another platform, not installed
    }

    const depManifest = require(depManifestPath);
    const oses = [].concat(depManifest.os || []);
    const cpus = [].concat(depManifest.cpu || []);
    if (oses.length && !oses.includes(process.platform)) continue;
    if (cpus.length && !cpus.includes(process.arch)) continue;

    const bin = depManifest.bin;
    const binFile = typeof bin === 'string' ? bin : Object.values(bin || {})[0];
    if (!binFile) continue;

    return path.join(path.dirname(depManifestPath), binFile);
  }

  return null;
}

// Forward termination signals to the child while it is running. Returns a
// function that detaches the handlers so the wrapper can re-raise a signal
// against itself without re-entering them.
function forwardSignals(child) {
  const handlers = new Map();
  for (const signal of FORWARDED_SIGNALS) {
    const handler = () => {
      try {
        child.kill(signal);
      } catch {
        // child already exited; nothing to forward to
      }
    };
    handlers.set(signal, handler);
    process.on(signal, handler);
  }

  return () => {
    for (const [signal, handler] of handlers) {
      process.removeListener(signal, handler);
    }
  };
}

function main() {
  const binaryPath = resolveBinaryPath(__dirname);
  if (!binaryPath) {
    process.stderr.write(
      `lstk: no prebuilt binary found for ${process.platform}-${process.arch}\n`,
    );
    process.exit(1);
  }

  const child = spawn(binaryPath, process.argv.slice(2), {
    stdio: 'inherit',
    env: process.env,
  });
  const stopForwarding = forwardSignals(child);

  child.on('error', (err) => {
    stopForwarding();
    process.stderr.write(`lstk: failed to launch ${binaryPath}: ${err.message}\n`);
    process.exit(1);
  });

  child.on('exit', (code, signal) => {
    stopForwarding();
    if (signal) {
      // Re-raise without our handlers so the wrapper's own exit status reflects
      // the signal that terminated the child.
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code === null ? 1 : code);
  });
}

if (require.main === module) {
  main();
}

module.exports = { resolveBinaryPath, forwardSignals, FORWARDED_SIGNALS };
