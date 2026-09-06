// Exercise the generated formula without replacing a user's installed dashboard.
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const assert = require('node:assert/strict');
const {pathToFileURL} = require('node:url');
const {execFileSync} = require('node:child_process');
const env = {...process.env,HOMEBREW_NO_AUTO_UPDATE:'1',HOMEBREW_NO_INSTALL_CLEANUP:'1',HOMEBREW_NO_AUTOREMOVE:'1',HOMEBREW_NO_ANALYTICS:'1'};
const brew = (...args) => execFileSync('brew',args,{env,encoding:'utf8',timeout:180000});
const suffix = String(process.pid);
const tap = `codex/openclaw-smoke-${suffix}`;
const name = `openclaw-dashboard-smoke-${suffix}`;
const formulaName = `${tap}/${name}`;
const temp = fs.mkdtempSync(path.join(os.tmpdir(),'openclaw-brew-smoke-'));
let tapped = false, installed = false;
try {
  const generated = fs.readFileSync('dist/release/homebrew/Formula/openclaw-dashboard.rb','utf8');
  // Change identity and download location only; retain generated installation/test logic.
  const formula = generated.replace('class OpenclawDashboard < Formula',`class OpenclawDashboardSmoke${suffix} < Formula\n  keg_only "isolated release verification fixture"`)
    .replace(/pkgshare/g,'(share/"openclaw-dashboard")')
    .replace(/https:\/\/github.com\/mudrii\/openclaw-dashboard\/releases\/download\/[^/]+\/(openclaw-dashboard-[^"\s]+\.tar\.gz)/g,
      (_,archive)=>pathToFileURL(path.resolve('dist/release',archive)).href);
  assert.notEqual(formula,generated);
  brew('tap-new','--no-git',tap); tapped = true;
  const repo = brew('--repository',tap).trim();
  fs.writeFileSync(path.join(repo,'Formula',`${name}.rb`),formula);
  installed = true; console.log(brew('install',formulaName));
  console.log(brew('test',formulaName));
  const prefix = brew('--prefix',formulaName).trim();
  const state = path.join(temp,'.openclaw');fs.mkdirSync(state,{recursive:true});
  const runtime = path.join(state,'dashboard');fs.mkdirSync(runtime);
  fs.writeFileSync(path.join(state,'openclaw.json'),'{}');
  fs.writeFileSync(path.join(runtime,'config.json'),JSON.stringify({timezone:'Asia/Kuala_Lumpur',openclaw:{mode:'native',binary:'/missing-brew-smoke-fixture'},ai:{enabled:false},system:{enabled:false}}));
  execFileSync(path.join(prefix,'bin/openclaw-dashboard'),['--refresh'],{cwd:temp,
    env:{PATH:process.env.PATH,HOME:temp,OPENCLAW_STATE_DIR:state},timeout:30000,stdio:'pipe'});
  const data = JSON.parse(fs.readFileSync(path.join(runtime,'data.json'),'utf8'));
  assert.equal(data.timezone,'Asia/Kuala_Lumpur');
  for(const asset of ['themes.json','VERSION','refresh.sh','examples/config.minimal.json']) assert.ok(fs.existsSync(path.join(runtime,asset)),`missing seeded ${asset}`);
  assert.equal(fs.statSync(path.join(runtime,'data.json')).mode & 0o777,0o600);
  console.log('Homebrew smoke passed: generated formula install/test, seeded assets, isolated refresh, private snapshot');
} finally {
  try {if(installed) brew('uninstall',formulaName);} finally {
    if(tapped) brew('untap',tap);
    fs.rmSync(temp,{recursive:true,force:true});
  }
}
