// Finds the binary that the matching platform package installed.
//
// Normally require.resolve finds it: the platform package sits in node_modules
// beside this one. The fallbacks cover layouts where it does not, such as pnpm's
// store and local `file:` installs, where this package is reached through a
// symlink and so resolves from its real location rather than the project's.
const fs = require('node:fs');
const path = require('node:path');

// npm's own platform names, which are what the platform packages are keyed by.
const SUPPORTED = new Set([
  'linux-x64', 'linux-arm64', 'linux-arm', 'linux-ia32',
  'darwin-x64', 'darwin-arm64',
  'win32-x64', 'win32-arm64', 'win32-ia32',
]);

function packageName() {
  return `px1-cli-${process.platform}-${process.arch}`;
}

function exeName() {
  return process.platform === 'win32' ? 'px1.exe' : 'px1';
}

function candidates(name) {
  const out = [];
  try {
    out.push(path.dirname(require.resolve(`${name}/package.json`)));
  } catch {}
  try {
    // Resolve as the project would, not as this file's real location does.
    out.push(path.dirname(require.resolve(`${name}/package.json`, {
      paths: [process.cwd(), path.join(__dirname, '..'), __dirname],
    })));
  } catch {}
  // A sibling inside the same node_modules, and the same walking up the tree.
  out.push(path.join(__dirname, '..', '..', name));
  for (let dir = __dirname; ; ) {
    out.push(path.join(dir, 'node_modules', name));
    const up = path.dirname(dir);
    if (up === dir) break;
    dir = up;
  }
  return out;
}

function binaryPath() {
  if (!SUPPORTED.has(`${process.platform}-${process.arch}`)) return null;
  for (const dir of candidates(packageName())) {
    const bin = path.join(dir, 'bin', exeName());
    try {
      if (fs.statSync(bin).isFile()) return bin;
    } catch {}
  }
  return null;
}

module.exports = { binaryPath, packageName, SUPPORTED };
