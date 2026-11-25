const { chromium } = require('playwright');
const assert = require('node:assert/strict');

(async () => {
  const baseURL = process.env.AUTORESEARCH_UI_URL || 'http://127.0.0.1:8081';
  const apiURL = process.env.AUTORESEARCH_API || 'http://127.0.0.1:8080/api/v1';
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  try {
    await page.goto(`${baseURL}/events`, { waitUntil: 'networkidle' });
    await page.waitForFunction(() => document.querySelectorAll('#event-list li').length > 5);
    assert.ok(await page.locator('#event-list li').count() > 5);
    await page.goto(`${baseURL}/graph`, { waitUntil: 'networkidle' });
    await page.waitForSelector('#research-graph canvas');
    assert.ok(await page.locator('#research-graph canvas').count() > 0);
    await page.goto(`${baseURL}/budget`, { waitUntil: 'networkidle' });
    await page.waitForSelector('.js-plotly-plot');
    assert.ok(await page.locator('.js-plotly-plot').count() > 0);
    await page.goto(`${baseURL}/experiments`, { waitUntil: 'networkidle' });
    assert.match(await page.locator('body').innerText(), /verified/);
    await page.goto(`${baseURL}/playback`, { waitUntil: 'networkidle' });
    assert.match(await page.locator('body').innerText(), /Tasks at selected event/);
    await page.goto(`${baseURL}/agents`, { waitUntil: 'networkidle' });
    assert.match(await page.locator('body').innerText(), /Open worker-1/);
    await page.goto(`${baseURL}/agents/worker-1`, { waitUntil: 'networkidle' });
    assert.match(await page.locator('body').innerText(), /Agent task history/);
    await page.goto(`${baseURL}/`, { waitUntil: 'networkidle' });
    await page.locator('textarea[aria-label="Message to Chief Scientist"]').fill('Summarize verification status');
    await page.getByRole('button', { name: /send message/i }).click();
    await page.waitForFunction(() => document.body.innerText.includes('Operator message recorded'));
    const response = await page.request.get(`${apiURL}/messages`);
    const messages = await response.json();
    assert.ok(messages.messages.length >= 2);
    assert.deepEqual(errors, []);
    console.log('Browser acceptance passed: events, graph, budget, experiments, playback, fleet, and persistent chat');
  } finally {
    await browser.close();
  }
})();
