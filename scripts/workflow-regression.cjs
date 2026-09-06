// Execute the actual publication gate with fake GitHub responses, without network.
const fs = require('node:fs');
const assert = require('node:assert/strict');
const yaml = fs.readFileSync('.github/workflows/release.yml', 'utf8');
const section = yaml.split('      - name: Require successful main CI for this exact commit\n')[1]?.split('\n      - name:')[0];
assert.ok(section, 'release CI gate missing');
const script = section.split('          script: |\n')[1].split('\n').map(line => line.slice(12)).join('\n');
const runGate = new (Object.getPrototypeOf(async function(){}).constructor)('github', 'context', 'core', script);
(async () => {
  const good = {head_sha:'candidate',status:'completed',conclusion:'success'};
  for (const [name, runs, denied] of [
    ['exact successful commit', [good], false],
    ['missing run', [], true],
    ['other commit', [{...good,head_sha:'old'}], true],
    ['pending', [{...good,status:'in_progress',conclusion:null}], true],
    ['failure', [{...good,conclusion:'failure'}], true],
    ['cancelled', [{...good,conclusion:'cancelled'}], true],
    ['skipped', [{...good,conclusion:'skipped'}], true],
  ]) {
    const errors = [];
    const github = {rest:{actions:{listWorkflowRuns:async query => {
      assert.deepEqual(query,{owner:'owner',repo:'repo',workflow_id:'tests.yml',branch:'main',event:'push',head_sha:'candidate',per_page:1});
      return {data:{workflow_runs:runs}};
    }}}};
    await runGate(github,{repo:{owner:'owner',repo:'repo'},sha:'candidate'},{setFailed:message=>errors.push(message)});
    assert.equal(errors.length,denied?1:0,name);
    console.log('PASS release gate:',name);
  }
})().catch(error => {console.error(error);process.exitCode=1;});
