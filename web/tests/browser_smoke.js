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
    await page.addInitScript(() => {
      let cytoscapeFactory;
      Object.defineProperty(window, 'cytoscape', {
        configurable: true,
        get: () => cytoscapeFactory,
        set: factory => {
          cytoscapeFactory = new Proxy(factory, {
            apply(target, thisArg, args) {
              const graph = Reflect.apply(target, thisArg, args);
              window.__researchGraph = graph;
              return graph;
            },
          });
        },
      });
    });
    await page.goto(`${baseURL}/graph`, { waitUntil: 'networkidle' });
    await page.waitForSelector('#research-graph canvas');
    await page.waitForFunction(() => window.__researchGraph);
    const readGraph = () => page.evaluate(() => window.__researchGraph.elements().map(element => ({
      group: element.group(),
      data: element.data(),
      classes: element.classes(),
    })));
    const normalize = elements => elements.map(element => ({
      group: element.group,
      data: element.data,
      classes: (Array.isArray(element.classes) ? element.classes : (element.classes || '').split(/\s+/)).filter(Boolean).sort(),
    })).sort((left, right) => left.data.id.localeCompare(right.data.id));
    const normalizedResponse = await page.request.get(`${baseURL}/internal/graph/elements`);
    assert.equal(normalizedResponse.status(), 200);
    const normalizedElements = await normalizedResponse.json();
    assert.deepEqual(normalize(await readGraph()), normalize(normalizedElements));
    assert.ok(normalizedElements.some(element => element.group === 'edges' && element.data.source && element.data.target));
    const refreshedElements = structuredClone(normalizedElements);
    const refreshedNode = refreshedElements.find(element => element.group === 'nodes');
    assert.ok(refreshedNode);
    refreshedNode.data.label = 'SSE refreshed label';
    let refreshRequests = 0;
    await page.route('**/internal/graph/elements', route => {
      refreshRequests += 1;
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(refreshedElements),
      });
    });
    await page.evaluate(() => {
      window.AutoResearchLive.disconnect();
      window.EventSource = class {
        constructor() {
          this.listeners = {};
          window.__testEventSource = this;
        }
        addEventListener(type, listener) { this.listeners[type] = listener; }
        close() {}
      };
      const refresh = window.refreshResearchGraph;
      window.__testGraphRefresh = null;
      window.refreshResearchGraph = () => {
        window.__testGraphRefresh = refresh();
        return window.__testGraphRefresh;
      };
      window.AutoResearchLive.reset();
    });
    await page.evaluate(() => {
      window.__testEventSource.listeners['hypothesis.verified']({
        lastEventId: '1000000',
        data: JSON.stringify({ type: 'hypothesis.verified', actor: 'chief', timestamp: '2026-01-01T00:00:00Z' }),
      });
    });
    assert.ok(await page.evaluate(() => Boolean(window.__testGraphRefresh)), 'SSE event did not call refreshResearchGraph');
    await page.evaluate(() => window.__testGraphRefresh);
    assert.equal(refreshRequests, 1, 'SSE event did not request normalized graph elements');
    const displayedAfterRefresh = normalize(await readGraph());
    assert.deepEqual(displayedAfterRefresh, normalize(refreshedElements));
    assert.notDeepEqual(displayedAfterRefresh, normalize(normalizedElements));
    await page.unroute('**/internal/graph/elements');
    await page.route('**/internal/graph/elements', route => route.fulfill({ status: 503, body: 'unavailable' }));
    await page.evaluate(() => window.refreshResearchGraph());
    assert.deepEqual(await readGraph(), []);
    await page.unroute('**/internal/graph/elements');
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
