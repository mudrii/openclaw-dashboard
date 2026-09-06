// Optional real-browser acceptance test. No package is added to the dashboard.
// PLAYWRIGHT_MODULE=/path/to/playwright node scripts/runtime-browser-smoke.cjs
const assert = require('node:assert/strict');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

(async () => {
  const browser = await chromium.launch({ headless: true, channel: process.env.BROWSER_CHANNEL || 'chrome' });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const base = process.env.DASHBOARD_TEST_URL || 'http://127.0.0.1:8081';
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    // First boot can serve a legacy cache until pre-warm and the next UI poll.
    await page.waitForFunction(() => document.querySelector('#taskLedger tbody tr'), null, { timeout: 60000 });
    assert.match(await page.locator('#runtimeSource').innerText(), /container/);
    await page.waitForFunction(() => document.querySelector('#runtimeHealthPanelInner').textContent.includes('Task audit'));
    assert.equal(await page.locator('#operationPanel').isVisible(), false);
    assert.equal(await page.locator('#logSourceCron').isDisabled(), true);
    const count = await page.locator('#taskLedger tbody tr').count();
    assert.ok(count > 0);
    await page.locator('#taskSearch').fill('no-such-task-acceptance-check');
    assert.equal(await page.locator('#taskLedger tbody tr').count(), 0);
    await page.locator('#taskSearch').fill('');
    assert.equal(await page.locator('#taskLedger tbody tr').count(), count);
    await page.locator('[data-automation-id]').first().click();
    await page.waitForFunction(() => !document.querySelector('#runtimeDetailBody').textContent.includes('Loading…'));
    assert.match(await page.locator('#runtimeDetailBody').innerText(), /recorded runs/);
    await page.locator('[data-close-runtime]').click();
    await page.locator('[data-open-work]').click();
    await page.waitForFunction(() => !document.querySelector('#runtimeDetailBody').textContent.includes('Loading…'));
    assert.match(await page.locator('#runtimeDetailBody').innerText(), /native control UI/);
    await page.locator('[data-close-runtime]').click();
    const capability = await (await page.request.get(base + '/api/chat/status')).json();
    assert.equal(capability.inferenceVerified, false);
    await page.evaluate(() => OCUI.toggleChat());
    await page.waitForFunction(() => document.querySelector('#chatInput').disabled);
    assert.equal(await page.locator('#chatSend').isDisabled(), true);
    await page.evaluate(() => OCUI.toggleChat());
    await page.locator('#runtimeSource').scrollIntoViewIfNeeded();
    await page.screenshot({ path: process.env.DASHBOARD_SCREENSHOT || '/tmp/openclaw-dashboard-runtime.png' });
    assert.deepEqual(errors, []);
    await page.setViewportSize({width:390,height:844});
    await page.locator('#taskSearch').scrollIntoViewIfNeeded();
    assert.equal(await page.locator('#taskSearch').isVisible(),true);
    console.log(JSON.stringify({ browser: 'passed', taskRows: count, history: 'passed', progress: 'passed', disabledActions: 'passed', pageErrors: errors }));
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
