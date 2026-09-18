#!/usr/bin/env node
// Launcher for the px1 binary.
//
// The binary itself ships in one small per-platform package (px1-cli-<os>-<arch>);
// npm installs only the one that matches this machine, because each declares its
// own `os` and `cpu`. Nothing is downloaded after install, so px1 works behind a
// proxy, in an offline cache, and with `npm ci --ignore-scripts`.
const { spawnSync } = require('node:child_process');
const { binaryPath, packageName } = require('../lib/resolve.js');

const target = binaryPath();
if (!target) {
  const name = packageName();
  console.error(
    `px1 does not ship a binary for ${process.platform}-${process.arch}.\n` +
    `Expected the package ${name}. If that platform should be supported, ` +
    `please open an issue: https://github.com/kedarvartak/px1/issues`,
  );
  process.exit(1);
}

const res = spawnSync(target, process.argv.slice(2), { stdio: 'inherit' });
if (res.error) {
  console.error(`px1 failed to start: ${res.error.message}`);
  process.exit(1);
}
process.exit(res.status === null ? 1 : res.status);
