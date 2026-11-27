const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');

function loadOperatorToken() {
  const tokenFile = process.env.AUTORESEARCH_OPERATOR_TOKEN_FILE;
  if (!tokenFile) {
    const token = process.env.AUTORESEARCH_OPERATOR_TOKEN;
    if (!token) throw new Error('AUTORESEARCH_OPERATOR_TOKEN_FILE or AUTORESEARCH_OPERATOR_TOKEN is required');
    return token;
  }
  const stat = fs.lstatSync(tokenFile);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.uid !== process.getuid() || (stat.mode & 0o077) !== 0) {
    throw new Error('Operator token file must be an owner-only regular file');
  }
  const token = fs.readFileSync(tokenFile, 'utf8').replace(/\r?\n$/, '');
  if (!token || /\s/.test(token)) throw new Error('Operator token file must contain one token line');
  return token;
}

(async () => {
  const baseURL = process.env.AUTORESEARCH_UI_URL || 'http://127.0.0.1:8081';
  const apiURL = process.env.AUTORESEARCH_API || 'http://127.0.0.1:8080/api/v1';
  const operatorToken = loadOperatorToken();
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  try {
    const assertNoTokenLeak = async () => {
      const cookies = await page.context().cookies();
      assert.ok(!JSON.stringify(cookies).includes(operatorToken));
      const storage = await page.evaluate(() => ({
        local: Object.values(localStorage),
        session: Object.values(sessionStorage),
      }));
      assert.ok(!JSON.stringify(storage).includes(operatorToken));
    };
    await page.goto(`${baseURL}/events`, { waitUntil: 'networkidle' });
    assert.equal(page.url(), `${baseURL}/login`);
    const anonymousCookies = await page.context().cookies();
    const tokenInput = page.getByLabel('Operator token', { exact: true });
    assert.equal(await tokenInput.getAttribute('type'), 'password');
    assert.equal(await tokenInput.getAttribute('name'), 'operator_token');
    assert.equal(await tokenInput.getAttribute('autocomplete'), 'current-password');
    assert.equal(await page.locator('#operator-login').getAttribute('method'), 'post');
    assert.equal(await page.locator('#operator-login').getAttribute('action'), '/login');
    const toggle = page.getByRole('button', { name: 'Show password', exact: true });
    await toggle.click();
    assert.equal(await tokenInput.getAttribute('type'), 'text');
    assert.equal(await toggle.getAttribute('aria-pressed'), 'true');
    await toggle.click();
    assert.equal(await tokenInput.getAttribute('type'), 'password');
    assert.equal(await toggle.getAttribute('aria-pressed'), 'false');
    const wrongToken = 'invalid-browser-token-7d6c5b4a3210fedcba09876';
    await tokenInput.fill(wrongToken);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await page.getByRole('alert').waitFor();
    assert.equal(await page.getByRole('alert').textContent(), 'Invalid token. Try again.');
    assert.equal(page.url(), `${baseURL}/login`);
    assert.ok(!(await page.content()).includes(wrongToken));
    await tokenInput.fill(operatorToken);
    await Promise.all([
      page.waitForURL(`${baseURL}/`, { waitUntil: 'networkidle' }),
      page.getByRole('button', { name: 'Sign in', exact: true }).click(),
    ]);
    await assertNoTokenLeak();
    const authenticatedCookies = await page.context().cookies();
    const anonymousSession = anonymousCookies.find(cookie => cookie.name === 'session');
    const authenticatedSession = authenticatedCookies.find(cookie => cookie.name === 'session');
    assert.ok(anonymousSession && authenticatedSession);
    assert.notEqual(anonymousSession.value, authenticatedSession.value, 'Login must rotate the session cookie');
    const plantedContext = await browser.newContext();
    await plantedContext.addCookies(anonymousCookies);
    const plantedPage = await plantedContext.newPage();
    const plantedResponse = await plantedPage.request.get(`${baseURL}/runtime/graph`, { maxRedirects: 0 });
    assert.equal(plantedResponse.status(), 401, 'Pre-login cookie must not become authenticated');
    await plantedContext.close();

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
    const response = await page.request.get(`${apiURL}/messages`, { headers: { Authorization: `Bearer ${operatorToken}` } });
    const messages = await response.json();
    assert.ok(messages.messages.length >= 2);
    const messageCount = messages.messages.length;
    const existingContext = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await existingContext.addCookies(await page.context().cookies());
    const existingClient = await existingContext.newPage();
    existingClient.on('pageerror', error => errors.push(error.message));
    await existingClient.goto(`${baseURL}/`, { waitUntil: 'networkidle' });
    await Promise.all([
      page.waitForURL(`${baseURL}/login`),
      page.getByRole('button', { name: 'Log out', exact: true }).click(),
    ]);
    await existingClient.locator('textarea[aria-label="Message to Chief Scientist"]').fill('Revoked client must not send');
    await existingClient.getByRole('button', { name: /send message/i }).click();
    await existingClient.waitForTimeout(500);
    const afterRevocation = await page.request.get(`${apiURL}/messages`, {
      headers: { Authorization: `Bearer ${operatorToken}` },
    });
    assert.equal((await afterRevocation.json()).messages.length, messageCount);
    await existingClient.close();
    const deniedAPI = await page.request.get(`${baseURL}/runtime/graph`, { maxRedirects: 0 });
    assert.equal(deniedAPI.status(), 401);
    const deniedPage = await page.request.get(`${baseURL}/`, { maxRedirects: 0 });
    assert.equal(deniedPage.status(), 303);
    assert.equal(deniedPage.headers().location, '/login');
    await assertNoTokenLeak();
    assert.deepEqual(errors, []);
    console.log('Browser acceptance passed: operator auth, revocation, events, graph, budget, experiments, playback, fleet, and chat');
  } finally {
    await browser.close();
  }
})();
