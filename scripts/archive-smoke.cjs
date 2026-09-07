// Check actual GoReleaser output, including an extracted native installation.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const {execFileSync} = require('node:child_process');

const dir = path.resolve('dist/release');
const archives = ['darwin-amd64', 'darwin-arm64', 'linux-amd64', 'linux-arm64']
  .map(target => `openclaw-dashboard-${target}.tar.gz`);
assert.deepEqual(fs.readdirSync(dir).filter(name => name.endsWith('.tar.gz')).sort(), archives.sort());
const checksums = new Map(fs.readFileSync(path.join(dir, 'checksums-sha256.txt'), 'utf8').trim().split('\n')
  .map(line => { const match = line.match(/^([a-f0-9]{64})\s+\*?(.+)$/); assert.ok(match, 'malformed checksum'); return [match[2], match[1]]; }));
for (const name of archives) {
  for (const artifact of [name, name + '.sbom.json']) {
    assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, artifact))).digest('hex'), checksums.get(artifact), `checksum: ${artifact}`);
  }
  const sbom = JSON.parse(fs.readFileSync(path.join(dir, name + '.sbom.json'), 'utf8'));
  assert.match(sbom.spdxVersion, /^SPDX-/);
  assert.ok(sbom.packages.some(pkg => pkg.name === 'github.com/mudrii/openclaw-dashboard'), `${name} SBOM omits dashboard`);
  assert.ok(sbom.packages.some(pkg => pkg.name === 'stdlib'), `${name} SBOM omits Go runtime`);
  const files = new Set(execFileSync('tar', ['-tzf', path.join(dir, name)], {encoding:'utf8'}).trim().split('\n'));
  for (const required of ['openclaw-dashboard', 'VERSION', 'README.md', 'LICENSE', 'install.sh', 'uninstall.sh',
    'assets/runtime/refresh.sh', 'assets/runtime/config.json', 'assets/runtime/themes.json',
    'docs/CONFIGURATION.md', 'docs/RUNTIME-COMPATIBILITY.md', 'examples/config.minimal.json', 'examples/config.full.json']) {
    assert.ok(files.has(required), `${name} missing ${required}`);
  }
}
const platform = process.platform === 'darwin' ? 'darwin' : process.platform === 'linux' ? 'linux' : null;
const arch = {x64:'amd64',arm64:'arm64'}[process.arch];
assert.ok(platform && arch, 'native archive smoke requires Linux/macOS amd64/arm64');
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'openclaw-archive-smoke-'));
try {
  execFileSync('tar', ['-xzf', path.join(dir, `openclaw-dashboard-${platform}-${arch}.tar.gz`), '-C', temp]);
  const env = {PATH:process.env.PATH, HOME:temp, OPENCLAW_DASHBOARD_DIR:temp, OPENCLAW_STATE_DIR:temp};
  const binary = path.join(temp, 'openclaw-dashboard');
  const version = JSON.parse(fs.readFileSync(path.join(dir, 'metadata.json'), 'utf8')).version;
  assert.ok(execFileSync(binary, ['--version'], {env,encoding:'utf8',timeout:10000}).includes(version), 'archive version stamp');
  assert.equal(fs.readFileSync(path.join(temp, 'VERSION'), 'utf8'), fs.readFileSync('VERSION', 'utf8'));
  fs.writeFileSync(path.join(temp, 'config.json'), JSON.stringify({timezone:'Asia/Kuala_Lumpur',
    openclaw:{mode:'native',binary:'/missing-openclaw-archive-fixture'}, ai:{enabled:false},system:{enabled:false}}));
  fs.writeFileSync(path.join(temp, 'openclaw.json'), '{}');
  execFileSync('/bin/bash', [path.join(temp, 'assets/runtime/refresh.sh')], {env,timeout:30000,stdio:'pipe'});
  const data = JSON.parse(fs.readFileSync(path.join(temp, 'data.json'), 'utf8'));
  assert.equal(data.timezone, 'Asia/Kuala_Lumpur');
  assert.equal(fs.statSync(path.join(temp, 'data.json')).mode & 0o777, 0o600);
  console.log('Archive smoke passed: four targets, checksums, SBOMs, runtime assets, native version, extracted refresh, timezone, private snapshot');
} finally {
  fs.rmSync(temp, {recursive:true,force:true});
}
