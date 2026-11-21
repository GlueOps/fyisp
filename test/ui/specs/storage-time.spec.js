// Details → Storage, a campaign's "Each round over time": the seeded past
// evening schedule (fyisp-webdev -storage history: 6 weekday evenings,
// 19:00 to 23:00 UTC, a round every 5 minutes; see pastSchedule), its
// metrics, a target alone, failures, the breaks between windows and a zoom.
// Its own fyisp-webdev, so the tests run one after another.
// Screenshot: out/shots/storage-time.png.
// @ts-check
const { test, expect, webdev } = require('./fixtures');
const AxeBuilder = require('@axe-core/playwright').default;
const { setTheme, WIDTHS, THEMES } = require('./home');

const PORT = 3232;
test.describe.configure({ mode: 'serial' });
/** @type {import('child_process').ChildProcess|undefined} */
let proc;
test.beforeAll(async () => {
  try {
    proc = await webdev(PORT, ['-verdict', 'ok', '-no-speed', '-storage', 'history']);
  } catch (e) {
    test.skip(/ENOENT/.test(String(e)), 'fyisp-webdev is not installed here (the uitest image has it)');
    throw e;
  }
});
test.afterAll(() => { proc?.kill(); });

/** Opens Storage with the past campaign shown. @param {import('@playwright/test').Page} page */
async function open(page) {
  const timings = page.waitForRequest((r) => /api\/storage\?.*part=timings/.test(r.url()));
  await page.goto(`http://127.0.0.1:${PORT}/#v=storage`);
  await timings;
  const chart = page.locator('.st-time');
  await expect(chart.locator('.u-over')).toHaveCount(1);
  await chart.scrollIntoViewIfNeeded();
  return chart;
}

test('storage over time: first byte for every target, one alone, and the speeds', async ({ page }) => {
  const chart = await open(page);
  const what = chart.getByRole('group', { name: 'What to show' });
  await expect(what.getByRole('button', { name: 'Time to first byte' })).toHaveAttribute('aria-pressed', 'true');
  const keys = chart.locator('.spt-legend button.spt-key');
  await expect(keys).toHaveCount(4);
  await expect(keys.nth(0)).toContainText(/AWS Virginia· 1\d\d ms/);
  await expect(keys.nth(3)).toContainText('Flaky edge');
  await expect(keys.nth(3)).toContainText('no keys: simpler request');
  // The NAS answers in 2 ms beside 190: too flat to read, and the chart says so.
  await expect(chart.locator('.spt-hint')).toHaveText(/^NAS is too small to read at this scale\./);
  await expect(chart.locator('.spt-corner')).toHaveText('one point per round · 5 minutes apart · the hours between windows are left out');
  await expect(chart.locator('.spt-chart')).toHaveAttribute('aria-label', /Backups \(R2\): usually 1\d\d ms, at worst .+, failed in \d+ rounds\./);
  // R2's failed rounds have a lane.
  await expect(chart.locator('.spt-lanes')).toBeVisible();
  await page.locator('.st-time').screenshot({ path: 'out/shots/storage-time.png' });

  // The axis: one window per evening, and the gap of the second evening inside it (a gap, not a break).
  const place = await page.evaluate(async () => {
    const { placeRounds } = await import('/static/storagetime.js');
    const c = await (await fetch('api/storage?part=campaign&id=1')).json();
    const p = placeRounds(c.rounds, c.campaign.interval_s * 1000);
    const g = c.rounds.findIndex((/** @type {any} */ r) => r.outcome === 'gap');
    return { wins: p.wins.length, gap: p.x[g], gapWin: p.wins[1] };
  });
  expect(place.wins).toBe(6);
  expect(place.gap).toBeGreaterThan(place.gapWin.x0);
  expect(place.gap).toBeLessThan(place.gapWin.x1);

  await chart.locator('.spt-hint').getByRole('button', { name: 'Show it alone' }).click();
  await expect(chart.getByRole('button', { name: /^Targets: NAS/ })).toBeVisible();
  await expect(chart.locator('.spt-legend')).toContainText('NAS· 2.');
  await expect(chart.locator('.spt-hint')).toBeHidden();
  // One target: the steps of a round in its tooltip.
  const box = /** @type {{x:number,y:number,width:number,height:number}} */ (await chart.locator('.u-over').boundingBox());
  // Early in the second evening, well before its gap.
  await page.mouse.move(box.x + box.width * 0.2, box.y + box.height - 10);
  await expect(page.locator('#tip')).toContainText(/^Round \d+ · /);
  await expect(page.locator('#tip')).toContainText(/Name lookup .+ ms · connecting .+ ms · encryption none/);
  await expect(page.locator('#tip')).toContainText('The median of 4 fresh connections.');
  await page.mouse.move(0, 0);

  await chart.getByRole('button', { name: 'Show all' }).click();
  await expect(keys).toHaveCount(4);
  // The flaky edge had no keys: no speeds for it.
  await what.getByRole('button', { name: 'Download speed' }).click();
  await expect(keys).toHaveCount(3);
  await expect(keys.nth(0)).toContainText('AWS Virginia· 6');
  await expect(chart.locator('.spt-cap')).toContainText('download of 64 MiB');
  await what.getByRole('button', { name: 'Upload speed' }).click();
  await expect(chart.locator('.spt-cap')).toContainText('upload of 16 MiB');
});

test('storage over time: a drag zooms in, a double click out; the picker', async ({ page }) => {
  const chart = await open(page);
  const box = /** @type {{x:number,y:number,width:number,height:number}} */ (await chart.locator('.u-over').boundingBox());
  const zoom = chart.getByRole('button', { name: 'Zoomed ×' });
  await expect(zoom).toBeHidden();
  await page.mouse.move(box.x + box.width * 0.5, box.y + 40);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.66, box.y + 40, { steps: 5 });
  await page.mouse.up();
  await expect(zoom).toBeVisible();
  await expect(page.locator('.st-time .u-select')).toHaveCSS('width', '0px');
  await page.mouse.dblclick(box.x + box.width * 0.5, box.y + 40);
  await expect(zoom).toBeHidden();

  await chart.getByRole('button', { name: /^Targets: all/ }).click();
  const dlg = page.getByRole('dialog', { name: 'Targets to show' });
  await dlg.getByRole('checkbox', { name: 'NAS' }).uncheck();
  await dlg.getByRole('checkbox', { name: 'Flaky edge' }).uncheck();
  await dlg.getByRole('button', { name: 'Show' }).click();
  await expect(chart.getByRole('button', { name: /^Targets: 2 of 4/ })).toBeVisible();
  await expect(chart.locator('.spt-legend button.spt-key')).toHaveText([/^AWS Virginia/, /^Backups \(R2\)/]);
  await expect(chart.locator('.spt-hint')).toBeHidden();
});

test('storage over time: axe, both themes, a phone', async ({ page }) => {
  await open(page);
  for (const w of WIDTHS) {
    await page.setViewportSize({ width: w, height: 900 });
    for (const theme of THEMES) {
      await setTheme(page, theme);
      await expect(page.locator('.st-time .u-over')).toHaveCount(1);
      const r = await new AxeBuilder({ page }).include('.st-time').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
      expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).slice(0, 4).join(' | ')}`), `${w}px ${theme}`).toEqual([]);
      const { sw, iw } = await page.evaluate(() => ({ sw: document.documentElement.scrollWidth, iw: window.innerWidth }));
      expect(sw, `${w}px: no sideways scrolling`).toBeLessThanOrEqual(iw);
    }
  }
});
