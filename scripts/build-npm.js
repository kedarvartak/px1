#!/usr/bin/env node
/**
 * scripts/build-npm.js
 *
 * Lays out the npm packages for a release, from the binaries build.sh has
 * already cross-compiled into dist/.
 *
 * The shape is the one esbuild popularised: one wrapper package (px1-cli) that
 * carries the `px1` command, and one tiny package per platform holding just the
 * binary. Each platform package declares its own `os` and `cpu`, so npm installs
 * only the one that matches the machine and skips the rest. Nothing is fetched
 * after install, which is what makes px1 work behind a proxy, from an offline
 * cache, and under `npm ci --ignore-scripts`.
 *
 * Usage: node scripts/build-npm.js [--version 0.1.6] [--dist dist] [--out dist-npm]
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');

function flag(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  return i !== -1 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
}

const version = flag('version', fs.readFileSync(path.join(root, 'VERSION'), 'utf8').trim());
const distDir = path.resolve(root, flag('dist', 'dist'));
const outDir = path.resolve(root, flag('out', 'dist-npm'));

// Go's names on the left, npm's on the right. Only the platforms people
// actually install px1 on are published; the rest stay download-only.
const TARGETS = [
  { go: 'linux-amd64', os: 'linux', cpu: 'x64' },
  { go: 'linux-arm64', os: 'linux', cpu: 'arm64' },
  { go: 'linux-arm', os: 'linux', cpu: 'arm' },
  { go: 'linux-386', os: 'linux', cpu: 'ia32' },
  { go: 'darwin-amd64', os: 'darwin', cpu: 'x64' },
  { go: 'darwin-arm64', os: 'darwin', cpu: 'arm64' },
  { go: 'windows-amd64', os: 'win32', cpu: 'x64' },
  { go: 'windows-arm64', os: 'win32', cpu: 'arm64' },
  { go: 'windows-386', os: 'win32', cpu: 'ia32' },
];

const shared = {
  version,
  homepage: 'https://github.com/kedarvartak/px1#readme',
  bugs: 'https://github.com/kedarvartak/px1/issues',
  repository: { type: 'git', url: 'git+https://github.com/kedarvartak/px1.git' },
  license: 'MIT',
};

fs.rmSync(outDir, { recursive: true, force: true });
fs.mkdirSync(outDir, { recursive: true });

const optional = {};
const built = [];

for (const t of TARGETS) {
  const exe = t.os === 'win32' ? '.exe' : '';
  const src = path.join(distDir, `px1-${version}-${t.go}${exe}`);
  if (!fs.existsSync(src)) {
    console.warn(`[build-npm] skipping ${t.os}-${t.cpu}: ${path.relative(root, src)} is missing`);
    continue;
  }
  const name = `px1-cli-${t.os}-${t.cpu}`;
  const pkg = path.join(outDir, name);
  fs.mkdirSync(path.join(pkg, 'bin'), { recursive: true });
  fs.copyFileSync(src, path.join(pkg, 'bin', `px1${exe}`));
  fs.chmodSync(path.join(pkg, 'bin', `px1${exe}`), 0o755);
  fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({
    name,
    ...shared,
    description: `px1 binary for ${t.os} ${t.cpu}. Installed automatically by px1-cli.`,
    os: [t.os],
    cpu: [t.cpu],
    files: ['bin'],
  }, null, 2) + '\n');
  fs.writeFileSync(path.join(pkg, 'README.md'),
    `# ${name}\n\nThe px1 binary for ${t.os} ${t.cpu}. Install [px1-cli](https://www.npmjs.com/package/px1-cli) instead:\n\n\`\`\`sh\nnpx px1-cli\n\`\`\`\n`);
  optional[name] = version;
  built.push(name);
}

if (built.length === 0) {
  console.error(`[build-npm] no binaries found in ${path.relative(root, distDir)}/ — run ./build.sh first`);
  process.exit(1);
}

// The wrapper: sources copied as they are, with the version and the platform
// packages it may pull in written in.
const wrapperSrc = path.join(root, 'npm', 'px1-cli');
const wrapperOut = path.join(outDir, 'px1-cli');
fs.cpSync(wrapperSrc, wrapperOut, { recursive: true });
const wrapper = JSON.parse(fs.readFileSync(path.join(wrapperSrc, 'package.json'), 'utf8'));
wrapper.version = version;
wrapper.optionalDependencies = optional;
fs.writeFileSync(path.join(wrapperOut, 'package.json'), JSON.stringify(wrapper, null, 2) + '\n');

console.log(`[build-npm] px1-cli@${version} with ${built.length} platform packages in ${path.relative(root, outDir)}/`);
for (const name of built) console.log(`  ${name}`);
