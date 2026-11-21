// Details → Speed, "Speed over time": the two plots, the range, a drag
// that zooms, the picker (every test together, or up to 8 regions and
// servers), the tooltip, failures and lower bounds. Its own fyisp-webdev
// with a week of speed tests (the shared one has two days), so the tests
// run one after another. Screenshots: out/shots/speed-time*.png.
// @ts-check
const { test, expect, webdev } = require('./fixtures');
const AxeBuilder = require('@axe-core/playwright').default;
const { setTheme, WIDTHS, THEMES } = require('./home');

const PORT = 3231;
const series = (/** @type {import('@playwright/test').Request} */ r) => /api\/speed\?.*part=series/.test(r.url());
const testsReq = (/** @type {import('@playwright/test').Request} */ r) => /api\/speed\?.*part=tests/.test(r.url());
const esc = (/** @type {string} */ s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const param = (/** @type {import('@playwright/test').Request} */ r, /** @type {string} */ k) => new URL(r.url()).searchParams.get(k);

test.describe.configure({ mode: 'serial' });
/** @type {import('child_process').ChildProcess|undefined} */
let proc;
test.beforeAll(async () => {
  try {
    proc = await webdev(PORT, ['-verdict', 'ok', '-speed-history', '168h']);
  } catch (e) {
    test.skip(/ENOENT/.test(String(e)), 'fyisp-webdev is not installed here (the uitest image has it)');
    throw e;
  }
});
test.afterAll(() => { proc?.kill(); });

/** Opens Speed and waits for the chart's first read. @param {import('@playwright/test').Page} page */
async function open(page) {
  const first = page.waitForRequest(series);
  await page.goto(`http://127.0.0.1:${PORT}/#v=speed`);
  return first;
}

/** The download plot, scrolled into view (it starts near the fold). @param {import('@playwright/test').Page} page */
async function plot(page) {
  const over = page.locator('.spt .u-over').first();
  await over.scrollIntoViewIfNeeded();
  return /** @type {{x:number,y:number,width:number,height:number}} */ (await over.boundingBox());
}

/** A part=series answer of 4 two-hour points ending now. @param {object[]} lines */
function answer(lines) {
  const step = 2 * 3600e3, t0 = Date.now() - 4 * step;
  return { json: { at: Date.now(), from: t0, to: t0 + 4 * step, step, t: [0, 1, 2, 3].map((i) => t0 + i * step), end: t0 + 4 * step, down_max: 9.4e8, up_max: 4.1e7, series: lines } };
}
const zeros = [0, 0, 0, 0];

test('speed over time: every test together, a range and a zoom', async ({ page }) => {
  const r = await open(page);
  expect(param(r, 'from')).toBe('now-7d');
  expect(param(r, 'targets')).toBeNull();
  await expect(page.locator('.spt .u-over')).toHaveCount(2);
  await expect(page.getByRole('heading', { name: 'Speed over time: all regions' })).toBeVisible();
  const chart = page.locator('.spt-chart');
  await expect(chart).toHaveAttribute('aria-label', /Your faster tests: download usually about [\d.]+ [MG]bit\/s, lowest about/);
  await expect(page.locator('.spt-corner')).toHaveText(/^2 h per point · about \d+ tests each$/);
  await page.locator('.spt').screenshot({ path: 'out/shots/speed-time.png' });

  // A drag on either plot asks for that window, in milliseconds, once; the window has tests.
  const range = page.getByRole('group', { name: 'Range' });
  const zoom = page.locator('.spt-zoom');
  await expect(zoom).toBeHidden();
  const box = await plot(page);
  let asked = 0;
  const count = (/** @type {import('@playwright/test').Request} */ q) => { if (series(q)) asked++; };
  page.on('request', count);
  const zoomed = page.waitForRequest(series);
  await page.mouse.move(box.x + box.width * 0.4, box.y + 40);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.8, box.y + 50, { steps: 5 });
  await page.mouse.up();
  const rz = await zoomed;
  const span = Number(param(rz, 'to')) - Number(param(rz, 'from'));
  expect(span).toBeGreaterThan(2 * 86400e3);
  expect(span).toBeLessThan(4 * 86400e3);
  await expect(zoom).toHaveText(/^Zoomed: .+ to .+ ×$/);
  await expect(range.getByRole('button', { name: '7d' })).toHaveAttribute('aria-pressed', 'false');
  await expect(chart).toHaveAttribute('aria-label', /download usually about/);
  await expect(page.locator('.spt .u-select').first()).toHaveCSS('width', '0px');
  expect(asked).toBe(1);
  page.off('request', count);

  // A double click zooms out; so does the button, which leaves the focus on the range.
  const [out] = await Promise.all([page.waitForRequest(series), page.mouse.dblclick(box.x + box.width * 0.5, box.y + 40)]);
  expect(param(out, 'from')).toBe('now-7d');
  await expect(zoom).toBeHidden();

  // Another range is asked for, and says so; a long one has a point a day.
  const [r90] = await Promise.all([page.waitForRequest(series), range.getByRole('button', { name: '90d' }).click()]);
  expect(param(r90, 'from')).toBe('now-90d');
  await expect(range.getByRole('button', { name: '90d' })).toHaveAttribute('aria-pressed', 'true');
  await expect(range.getByRole('button', { name: '90d' })).toBeFocused();
  await expect(page.locator('.spt-corner')).toHaveText(/^1 day per point · about \d+ tests each · slow evenings are averaged into the day$/);
});

test('speed over time: pick regions and servers, up to 8', async ({ page }) => {
  await open(page);
  await page.getByRole('button', { name: /^Tests to: All regions/ }).click();
  const dlg = page.getByRole('dialog', { name: 'Tests to show' });
  const together = dlg.getByRole('checkbox', { name: 'All regions together (one line)' });
  await expect(together).toBeChecked();
  // A search narrows the list to a region and its servers.
  await dlg.getByRole('searchbox').fill('london');
  await expect(dlg.getByRole('checkbox', { name: 'Clouvider London' })).toBeVisible();
  await expect(dlg.getByRole('checkbox', { name: 'US East: every server' })).toBeHidden();
  await dlg.getByRole('searchbox').fill('');
  await dlg.getByRole('checkbox', { name: 'UK: every server' }).check();
  await dlg.getByRole('checkbox', { name: 'Clouvider London' }).check();
  await expect(together).not.toBeChecked();
  await expect(dlg.getByRole('status')).toHaveText('2 of 8 chosen');
  // The ninth can't be ticked.
  const boxes = dlg.locator('.sp-pick-list input[type=checkbox]');
  for (let i = 4; i < 10; i++) await boxes.nth(i).check();
  await expect(dlg.getByRole('status')).toHaveText('8 of 8 chosen');
  await expect(boxes.nth(11)).toBeDisabled();
  for (let i = 4; i < 10; i++) await boxes.nth(i).uncheck();

  let perTest = 0;
  page.on('request', (q) => { if (testsReq(q)) perTest++; });
  const [r] = await Promise.all([page.waitForRequest(series), dlg.getByRole('button', { name: 'Show' }).click()]);
  expect(param(r, 'targets')).toBe('uk,Clouvider-London');
  await expect(page.getByRole('heading', { name: 'Speed over time: 2 targets' })).toBeVisible();
  const keys = page.locator('.spt-legend button.spt-key');
  await expect(keys).toHaveText(['UK', 'Clouvider London']);
  await expect(page.locator('.spt-corner')).toHaveText(/^2 targets · \d+ h per point$/);
  expect(perTest, 'several targets: lines, never each test').toBe(0);

  // A legend entry shows that line alone, and hands the focus to the picker's button.
  const [alone] = await Promise.all([page.waitForRequest(series), keys.nth(1).click()]);
  expect(param(alone, 'targets')).toBe('Clouvider-London');
  await expect(page.getByRole('heading', { name: 'Speed over time: Clouvider London' })).toBeVisible();
  const pick = page.getByRole('button', { name: /^Tests to: Clouvider London/ });
  await expect(pick).toBeFocused();

  // Cancel keeps the choice; "together" goes back to one line.
  await pick.click();
  await together.check();
  await dlg.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByRole('heading', { name: 'Speed over time: Clouvider London' })).toBeVisible();
  await pick.click();
  await together.check();
  const [all] = await Promise.all([page.waitForRequest(series), dlg.getByRole('button', { name: 'Show' }).click()]);
  expect(param(all, 'targets')).toBeNull();
  // Back to one line of every test: its points are gone.
  await expect(page.locator('.spt-corner')).toHaveText(/^\d+ h per point · about \d+ tests each$/);
});

// The refused read logs a console error, which is the point.
test.describe('a failed read', () => {
  test.use({ failOk: [/part=series/] });
  test('speed over time: the tooltip reads a point; a failed read recovers', async ({ page }) => {
    let fail = false;
    await page.route(/api\/speed\?.*part=series/, (route) => (fail ? route.fulfill({ status: 500, json: { error: 'x', code: 'internal' } }) : route.fulfill(answer([{
      id: '', kind: 'pooled', label: '', usual: 30,
      down_bps: [9.1e8, null, null, 5.2e8], up_bps: [4e7, null, null, 3.9e7],
      n: [30, 4, 0, 29], up_n: [30, 4, 0, 29], failed: [0, 0, 28, 1], down_lb: zeros, up_lb: zeros }]))));
    await open(page);
    await expect(page.locator('.spt .u-over')).toHaveCount(2);
    const box = await plot(page);
    const tip = page.locator('#tip');
    const at = (/** @type {number} */ i) => page.mouse.move(box.x + box.width * (i + 0.5) / 4, box.y + 30);
    await at(0);
    await expect(tip).toContainText('910 Mbit/s down · 40 Mbit/s up');
    await expect(tip).toContainText('30 tests finished');
    await at(1);
    await expect(tip).toContainText('too few tests to compare');
    await at(2);
    await expect(tip).toContainText('none finished');
    await expect(tip).toContainText('0 tests finished, 28 didn’t');
    await at(3);
    await expect(tip).toContainText('520 Mbit/s down');
    await expect(page.locator('.spt-chart')).toHaveAttribute('aria-label', /lowest about 520 Mbit\/s/);

    // One failed read: a sentence instead of the chart, and the next read brings it back.
    fail = true;
    const range = page.getByRole('group', { name: 'Range' });
    await range.getByRole('button', { name: '30d' }).click();
    await expect(page.locator('.spt')).toContainText('Couldn’t load the speed tests over time.');
    await expect(page.locator('.spt-chart')).toBeHidden();
    await expect(range.getByRole('button', { name: '30d' })).toHaveAttribute('aria-pressed', 'true');
    fail = false;
    const [again] = await Promise.all([page.waitForRequest(series), range.getByRole('button', { name: '48h' }).click()]);
    expect(param(again, 'from')).toBe('now-48h');
    await expect(page.locator('.spt-chart')).toBeVisible();
    await expect(page.locator('.spt .u-over')).toHaveCount(2);
  });
});

test('speed over time: no tests yet', async ({ page }) => {
  await page.route(/api\/speed\?.*part=series/, (route) => route.fulfill(answer([{ id: '', kind: 'pooled', label: '', usual: 0,
    down_bps: [null, null, null, null], up_bps: [null, null, null, null], n: zeros, up_n: zeros, failed: zeros, down_lb: zeros, up_lb: zeros }])));
  await open(page);
  await expect(page.locator('.spt')).toContainText('No scheduled tests in this range yet.');
  await expect(page.locator('.spt-chart')).toBeHidden();
  await expect(page.locator('.spt-corner')).toHaveText('');
});

test('speed over time: several lines, lower bounds and failures', async ({ page }) => {
  await page.route(/api\/speed\?.*part=series/, (route) => route.fulfill(answer([
    { id: 'uk', kind: 'region', label: 'UK', usual: 6, down_bps: [9e8, 9.1e8, null, 8.8e8], up_bps: [4e7, 4e7, null, 4e7],
      n: [6, 6, 0, 6], up_n: [6, 6, 0, 6], failed: [0, 0, 6, 0], down_lb: zeros, up_lb: zeros },
    { id: 'oceania', kind: 'region', label: 'Oceania', usual: 6, down_bps: [1.5e8, 1.6e8, 1.5e8, 2e8], up_bps: [3e7, 3e7, 3e7, 3e7],
      n: [6, 6, 6, 6], up_n: [6, 6, 6, 6], failed: zeros, down_lb: [5, 5, 4, 0], up_lb: zeros },
  ])));
  await open(page);
  await expect(page.locator('.spt .u-over')).toHaveCount(2);
  const keys = page.locator('.spt-legend button.spt-key');
  await expect(keys.nth(0)).toHaveText('UK');
  await expect(keys.nth(1)).toHaveText('Oceania' + 'often “at least”');
  await expect(page.locator('.spt-cap')).toContainText('Dotted: most tests were still speeding up when they ended');
  // UK's point where every test failed gets a lane under the plots.
  await expect(page.locator('.spt-lanes')).toBeVisible();
  const box = await plot(page);
  const tip = page.locator('#tip');
  await page.mouse.move(box.x + box.width * 0.5 / 4, box.y + 30);
  await expect(tip).toContainText('at least 150 Mbit/s down');
  await expect(tip).toContainText('900 Mbit/s down');
  await page.mouse.move(box.x + box.width * 2.5 / 4, box.y + 30);
  await expect(tip).toContainText('none finished');
  await expect(tip).toContainText('0 tests finished, 6 didn’t');
  await page.locator('.spt').screenshot({ path: 'out/shots/speed-time-lines.png' });
});

test('speed over time: opened from another view; axe, both themes, a phone', async ({ page }) => {
  // The chart reads its tests when Speed first shows, not while it is hidden.
  await page.goto(`http://127.0.0.1:${PORT}/#v=c`);
  await expect(page.locator('.panel .u-over').first()).toBeVisible();
  const first = page.waitForRequest(series);
  await page.evaluate(() => { location.hash = 'v=speed'; });
  await first;
  await expect(page.locator('.spt .u-over')).toHaveCount(2);

  const axe = async (/** @type {string} */ sel) => (await new AxeBuilder({ page }).include(sel).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze())
    .violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 4).join(' | ')}`);
  for (const w of WIDTHS) {
    await page.setViewportSize({ width: w, height: 900 });
    for (const theme of THEMES) {
      // A theme switch draws the plots again, in its colours.
      const old = await page.locator('.spt .u-over').first().elementHandle();
      await setTheme(page, theme);
      await expect(page.locator('.spt .u-over')).toHaveCount(2);
      expect(await axe('.spt'), `${w}px ${theme}`).toEqual([]);
      const { sw, iw } = await page.evaluate(() => ({ sw: document.documentElement.scrollWidth, iw: window.innerWidth }));
      expect(sw, `${w}px: no sideways scrolling`).toBeLessThanOrEqual(iw);
      await old?.dispose();
    }
  }
  await page.getByRole('button', { name: /^Tests to:/ }).click();
  expect(await axe('dialog.dlg')).toEqual([]);
});

test('speed over time: one target shows every test as its own point', async ({ page }) => {
  await open(page);
  await page.getByRole('button', { name: /^Tests to: All regions/ }).click();
  const dlg = page.getByRole('dialog', { name: 'Tests to show' });
  await dlg.getByRole('checkbox', { name: 'Clouvider London' }).check();
  const tests = page.waitForRequest(testsReq);
  await dlg.getByRole('button', { name: 'Show' }).click();
  const rt = await tests;
  expect(param(rt, 'row')).toBe('Clouvider-London');
  expect(Number(param(rt, 'to')) - Number(param(rt, 'from'))).toBe(7 * 86400e3);
  await expect(page.locator('.spt-corner')).toHaveText(/^each point is one test · \d+ tests$/);
  await expect(page.locator('.spt-chart')).toHaveAttribute('aria-label', /Clouvider London: download usually about .+ \d+ tests, one point each\.$/);
  await expect(page.locator('.spt-legend')).toContainText('●Clouvider London');
  await expect(page.locator('.spt-cap')).toContainText('Every test to this target');

  // The selected row of the page has a button for the same.
  const rowName = await page.locator('#sp-row-h').innerText();
  await expect(page.locator('.sp-row').getByRole('heading', { name: /by hour of day/ })).toBeVisible();
  await expect(page.getByRole('heading', { name: /^Over the last/ })).toHaveCount(0);
  const again = page.waitForRequest(testsReq);
  await page.locator('.sp-row').getByRole('button', { name: 'Show over time' }).click();
  await again;
  await expect(page.getByRole('heading', { name: `Speed over time: ${rowName}` })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp(`^Tests to: ${esc(rowName)}`) })).toBeFocused();
  await expect(page.locator('.spt')).toBeInViewport();
});

test('speed over time: a test’s point says what it was', async ({ page }) => {
  const span = 8 * 3600e3, t0 = Date.now() - span;
  await page.route(/api\/speed\?.*part=series/, (route) => route.fulfill(answer([{ id: 'uk', kind: 'region', label: 'UK', usual: 6,
    down_bps: [9e8, 9e8, 9e8, 9e8], up_bps: [4e7, 4e7, 4e7, 4e7], n: [1, 1, 1, 1], up_n: [1, 1, 1, 1], failed: [0, 0, 1, 0], down_lb: zeros, up_lb: zeros }])));
  const at = (/** @type {number} */ f) => t0 + f * span;
  await page.route(/api\/speed\?.*part=tests/, (route) => route.fulfill({ json: {
    at: Date.now(), from: t0, to: t0 + span, id: 'uk', kind: 'region', label: 'UK', capped: false, complete_from: t0,
    servers: { 'Leaseweb-London': { label: 'Leaseweb London', provider: 'Leaseweb' }, 'Clouvider-London': { label: 'Clouvider London', provider: 'Clouvider' } },
    // Leaseweb first among the tests; the UK region lists Clouvider first, so Clouvider keeps ● as in the row's chart.
    tests: [
      { id: 1, start: at(0.2), outcome: 'ok', server: 'Leaseweb-London', down_bps: 8e8, up_bps: 4e7 },
      { id: 2, start: at(0.4), outcome: 'ok', server: 'Clouvider-London', down_bps: 2e8, up_bps: 2e7, down_lb: true, fallback: true },
      { id: 3, start: at(0.6), outcome: 'busy', server: 'Leaseweb-London', down_bps: null, up_bps: null },
      { id: 4, start: at(0.8), outcome: 'ok', server: 'Leaseweb-London', down_bps: 5e8, up_bps: null, on_demand: true, capped_by_server: true },
    ] } }));
  await open(page);
  // The tests' fastest (800 Mbit/s) and down_max (940) put the axis top at 1000 (niceCeil of 940 × 1.03): the hovers below count on it.
  await expect(page.locator('.spt-corner')).toHaveText('each point is one test · 4 tests');
  const legend = page.locator('.spt-legend');
  await expect(legend).toContainText('●Clouvider London▲Leaseweb London');
  await expect(legend).toContainText('hollow: a backup server ran it');
  await expect(legend).toContainText('↑at least');
  await expect(legend).toContainText('ringed: you asked for it');
  const box = await plot(page);
  const tip = page.locator('#tip');
  // The axis tops at 1000 Mbit/s (the fastest test, rounded up); a test with no result sits on the baseline.
  const hover = (/** @type {number} */ f, /** @type {number|null} */ mbps) => page.mouse.move(box.x + box.width * f, mbps == null ? box.y + box.height - 5 : box.y + box.height * (1 - mbps / 1000));
  await hover(0.2, 800);
  await expect(tip).toContainText('Leaseweb London');
  await expect(tip).toContainText('800 Mbit/s down · 40 Mbit/s up');
  await hover(0.4, 200);
  await expect(tip).toContainText('Clouvider London (a backup server)');
  await expect(tip).toContainText('at least 200 Mbit/s down');
  await hover(0.6, null);
  await expect(tip).toContainText('server busy');
  await hover(0.8, 500);
  await expect(tip).toContainText('you asked for it, as fast as the server goes');
  await expect(tip).toContainText('500 Mbit/s down · the upload didn’t finish');
  await page.locator('.spt').screenshot({ path: 'out/shots/speed-time-tests.png' });
});

test('speed over time: a target with too many tests is a line until a zoom brings each test', async ({ page }) => {
  const span = 30 * 86400e3, now = Date.now();
  /** n tests in each of 4 points of the window asked for. @param {URL} u @param {number} n */
  const lines = (u, n) => {
    const to = Number(u.searchParams.get('to')) || now, from = /^\d+$/.test(u.searchParams.get('from') || '') ? Number(u.searchParams.get('from')) : to - span;
    const step = (to - from) / 4;
    return { json: { at: now, from, to, step, t: [0, 1, 2, 3].map((i) => from + i * step), end: to, down_max: 9e8, up_max: 4e7,
      series: [{ id: 'uk', kind: 'region', label: 'UK', usual: n, down_bps: [9e8, 9e8, 9e8, 9e8], up_bps: [4e7, 4e7, 4e7, 4e7],
        n: [n, n, n, n], up_n: [n, n, n, n], failed: zeros, down_lb: zeros, up_lb: zeros }] } };
  };
  await page.route(/api\/speed\?.*part=series/, (route) => {
    const u = new URL(route.request().url());
    // The whole month: 3000 tests; a zoomed window: 40.
    route.fulfill(lines(u, /^\d+$/.test(u.searchParams.get('from') || '') ? 10 : 750));
  });
  await page.route(/api\/speed\?.*part=tests/, (route) => {
    const u = new URL(route.request().url()), from = Number(u.searchParams.get('from')), to = Number(u.searchParams.get('to'));
    route.fulfill({ json: { at: now, from, to, id: 'uk', kind: 'region', label: 'UK', capped: true, complete_from: from + 1,
      servers: { A: { label: 'Clouvider London', provider: 'Clouvider' } },
      tests: [1, 2, 3].map((i) => ({ id: i, start: from + i * (to - from) / 4, outcome: 'ok', server: 'A', down_bps: 9e8, up_bps: 4e7 })) } });
  });
  let perTest = 0;
  page.on('request', (q) => { if (testsReq(q)) perTest++; });
  await open(page);
  await page.getByRole('button', { name: /^Tests to: All regions/ }).click();
  const dlg = page.getByRole('dialog', { name: 'Tests to show' });
  await dlg.getByRole('checkbox', { name: 'UK: every server' }).check();
  await dlg.getByRole('button', { name: 'Show' }).click();
  await expect(page.locator('.spt-corner')).toHaveText(/ · zoom in to see each test$/);
  expect(perTest).toBe(0);

  const box = await plot(page);
  const zoomed = page.waitForRequest(testsReq);
  await page.mouse.move(box.x + box.width * 0.3, box.y + 40);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.6, box.y + 40, { steps: 5 });
  await page.mouse.up();
  const rt = await zoomed;
  expect(param(rt, 'row')).toBe('uk');
  expect(Number(param(rt, 'to')) - Number(param(rt, 'from'))).toBeLessThan(span / 2);
  // A capped answer says it holds the newest tests only.
  await expect(page.locator('.spt-corner')).toHaveText('each point is one test · the newest 3 tests');
  expect(perTest).toBe(1);
});
