// Details → Charts: the target filter. The text is a regular expression
// shared with the Overview (#oq=): only the charts with a matching target
// show, with only the matching targets' lines. A pattern that isn't valid,
// or takes too long, leaves the last result and says so.
// @ts-check
const { test, expect } = require('./fixtures');
const AxeBuilder = require('@axe-core/playwright').default;

/** The group charts on screen (not the network path's). @param {import('@playwright/test').Page} page */
const charts = (page) => page.locator('section[id^="panel-"]:not(.path):visible');
/** @param {import('@playwright/test').Page} page @param {string} group */
const legend = (page, group) => page.locator(`#panel-${group} .legend .name`);
const box = (/** @type {import('@playwright/test').Page} */ page) => page.getByRole('searchbox', { name: 'Filter charts' });
const count = (/** @type {import('@playwright/test').Page} */ page) => page.locator('.cf-count');

test('a pattern from the link: matching charts and lines only, and no request for the others', async ({ page }) => {
  /** @type {string[]} */
  const groups = [];
  page.on('request', (r) => { const u = new URL(r.url()); if (u.pathname.endsWith('/api/panel')) groups.push(u.searchParams.get('group') || ''); });
  await page.goto('/#v=c&oq=' + encodeURIComponent('zoom|slack'));
  await expect(count(page)).toHaveText('2 targets in 1 of 4 charts');
  await expect(charts(page)).toHaveCount(1);
  await expect(page.locator('#panel-call-apps .u-over')).toBeVisible();
  await expect(legend(page, 'call-apps')).toHaveText(['Zoom', 'Slack']);
  await expect(box(page)).toHaveValue('zoom|slack');
  await expect(page.getByRole('button', { name: 'Clear filter' })).toBeVisible();
  expect(new Set(groups)).toEqual(new Set(['path', 'call-apps']));
});

test('typing: several at once, an empty part at the end, one match shows its probes', async ({ page }) => {
  await page.goto('/#v=c');
  await expect(charts(page)).toHaveCount(4);
  await expect(count(page)).toHaveText('');
  await box(page).fill('zoom|github|netflix|');
  await expect(count(page)).toHaveText('3 targets in 3 of 4 charts');
  await expect(charts(page)).toHaveCount(3);
  await expect(legend(page, 'work')).toHaveText(['dev-GitHub']);
  await expect(page).toHaveURL(/oq=zoom%7Cgithub%7Cnetflix(&|$)/);
  // Anchors and classes work: it is a regular expression.
  await box(page).fill('^cdn-(akamai|fastly)$');
  await expect(count(page)).toHaveText('2 targets in 1 of 4 charts');
  await expect(legend(page, 'web')).toHaveText(['CDN-Akamai', 'CDN-Fastly']);
  // One target on a chart: "Probes over X ms", as when it is isolated by hand.
  await box(page).fill('netflix');
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await expect(page.locator('#panel-web .probes')).toBeVisible();
});

test('Clear filter brings every chart back at its width, and the box keeps focus', async ({ page }) => {
  await page.goto('/#v=c&oq=zoom');
  await expect(charts(page)).toHaveCount(1);
  await page.getByRole('button', { name: 'Clear filter' }).click();
  await expect(charts(page)).toHaveCount(4);
  await expect(box(page)).toBeFocused();
  await expect(box(page)).toHaveValue('');
  await expect(page.getByRole('button', { name: 'Clear filter' })).toBeHidden();
  await expect(page).not.toHaveURL(/oq=/);
  await expect(page.locator('#panel-web .u-over')).toBeVisible();
  expect((await page.locator('#panel-web .u-over').boundingBox())?.width).toBeGreaterThan(300);
  await expect(legend(page, 'web')).toHaveCount(7);
});

test('no match, and a pattern that is markup: text, not elements', async ({ page }) => {
  const q = '<img src=x onerror=alert(1)>';
  await page.goto('/#v=c&oq=' + encodeURIComponent(q));
  await expect(page.locator('.cf-none')).toHaveText(`No target matches “${q}”.`);
  await expect(count(page)).toHaveText('0 targets in 0 of 4 charts');
  await expect(charts(page)).toHaveCount(0);
  await expect(page.locator('section.panel.path')).toBeVisible();
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
});

test('a pattern that isn’t valid keeps the last result and says so', async ({ page }) => {
  await page.goto('/#v=c');
  await box(page).fill('zoom');
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await expect(page.locator('#cf-note')).toContainText('regular expression');
  await box(page).fill('zoom|slack(');
  await expect(page.locator('#cf-note')).toContainText('isn’t valid');
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await box(page).fill('zoom|slack');
  await expect(count(page)).toHaveText('2 targets in 1 of 4 charts');
  await expect(page.locator('#cf-note')).toContainText('regular expression');
});

test('a pattern that takes too long is stopped, and the next one works', async ({ page }) => {
  await page.goto('/#v=c');
  await expect(charts(page)).toHaveCount(4);
  // Nested repeats that can't match: seconds of backtracking on an 18-letter name.
  await box(page).fill('^((.+)+)+!$');
  await expect(page.locator('#cf-note')).toContainText('takes too long');
  await expect(charts(page)).toHaveCount(4);
  await box(page).fill('zoom');
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await expect(page.locator('#cf-note')).toContainText('regular expression');
});

test('a matcher that is slow to load is waited for, not taken for a slow pattern', async ({ page }) => {
  await page.route('**/static/match.js', async (r) => { await new Promise((ok) => setTimeout(ok, 700)); await r.continue(); });
  await page.goto('/#v=c&oq=zoom');
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await expect(charts(page)).toHaveCount(1);
});

test.describe('without the matcher', () => {
  test.use({ failOk: [/match\.js/] });
  test('the text is matched as it is', async ({ page }) => {
    await page.route('**/static/match.js', (r) => r.fulfill({ status: 404, body: '' }));
    await page.goto('/#v=c&oq=zoom');
    await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
    await box(page).fill('zoom|slack'); // not a pattern here: no name has that text
    await expect(count(page)).toHaveText('0 targets in 0 of 4 charts');
  });
});

test('a target isolated by hand that the filter leaves out does not leave an empty chart', async ({ page }) => {
  await page.goto('/#v=c');
  await expect(legend(page, 'web')).toHaveCount(7);
  await page.locator('#panel-web .legend li', { hasText: 'Netflix' }).click();
  await expect(page.locator('#panel-web .legend li.off')).toHaveCount(6);
  await box(page).fill('cdn-');
  await expect(count(page)).toHaveText('5 targets in 1 of 4 charts');
  await expect(legend(page, 'web')).toHaveCount(5);
  await expect(page.locator('#panel-web .legend li.off')).toHaveCount(0);
  // Isolating under a filter, then clearing it: still that one target.
  await page.locator('#panel-web .legend li', { hasText: 'CDN-Fastly' }).click();
  await page.getByRole('button', { name: 'Clear filter' }).click();
  await expect(legend(page, 'web')).toHaveCount(7);
  await expect(page.locator('#panel-web .legend li:not(.off) .name')).toHaveText(['CDN-Fastly']);
});

test('a chart hidden while the window changed width is drawn at the new width', async ({ page }) => {
  await page.goto('/#v=c');
  await expect(page.locator('#panel-web .u-over')).toBeVisible();
  await box(page).fill('zoom');
  await expect(charts(page)).toHaveCount(1);
  await page.setViewportSize({ width: 900, height: 900 });
  await page.getByRole('button', { name: 'Clear filter' }).click();
  await expect(charts(page)).toHaveCount(4);
  const chart = await page.locator('#panel-web .chart').boundingBox();
  await expect.poll(async () => (await page.locator('#panel-web .u-wrap').boundingBox())?.width).toBeCloseTo(/** @type {{width:number}} */ (chart).width, 0);
});

test('Back and a long pattern from a link', async ({ page }) => {
  await page.goto('/#v=c&oq=zoom');
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await page.goto('/#v=c&oq=slack');
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await expect(legend(page, 'call-apps')).toHaveText(['Slack']);
  await page.goBack();
  await expect(legend(page, 'call-apps')).toHaveText(['Zoom']);
  await expect(box(page)).toHaveValue('zoom');
  // A pattern is cut at 100 characters.
  await page.goto('/#v=c&oq=' + 'zoom|'.repeat(60));
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  expect((await box(page).inputValue()).length).toBeLessThanOrEqual(100);
});

test('the Overview and Charts share the pattern', async ({ page }) => {
  await page.goto('/#v=o&oq=' + encodeURIComponent('zoom|slack'));
  await expect(page.locator('.ovt tbody tr:not(.none):visible')).toHaveCount(2);
  await expect(page.getByRole('searchbox', { name: 'Filter targets' })).toHaveValue('zoom|slack');
  await page.getByRole('button', { name: 'Charts', exact: true }).click();
  await expect(box(page)).toHaveValue('zoom|slack');
  await expect(count(page)).toHaveText('2 targets in 1 of 4 charts');
  await page.getByRole('button', { name: 'Overview', exact: true }).click();
  await page.getByRole('searchbox', { name: 'Filter targets' }).fill('cdn-');
  await expect(page.locator('.ovt tbody tr:not(.none):visible')).toHaveCount(5);
  // The Overview says what is wrong with a pattern too, to screen readers as well.
  await page.getByRole('searchbox', { name: 'Filter targets' }).fill('cdn-(');
  await expect(page.locator('#ov-note')).toContainText('isn’t valid');
  await expect(page.locator('span.sr-only[role=status]', { hasText: 'isn’t valid' })).toHaveCount(1);
  await expect(page.locator('.ovt tbody tr:not(.none):visible')).toHaveCount(5);
});

test('focus mode shows its target whatever the filter says', async ({ page }) => {
  await page.goto('/#v=c&oq=zoom&f=Netflix');
  await expect(page.locator('.focusbar')).toContainText('Netflix');
  await expect(page.locator('#panel-web')).toBeVisible();
  await expect(page.locator('#panel-call-apps')).toBeHidden();
  await expect(page.locator('.cfilter')).toBeHidden();
  await expect(legend(page, 'web')).toHaveCount(7);
  await page.getByRole('button', { name: 'Show all charts' }).click();
  await expect(count(page)).toHaveText('1 target in 1 of 4 charts');
  await expect(charts(page)).toHaveCount(1);
});

test('axe: the filter row, with matches and with none', async ({ page }) => {
  for (const q of ['zoom|slack', 'nothing-here']) {
    await page.goto('/#v=c&oq=' + encodeURIComponent(q));
    await expect(count(page)).toContainText('of 4 charts');
    const r = await new AxeBuilder({ page }).include('.cfilter').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']).analyze();
    expect(r.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(' | ')}`), q).toEqual([]);
  }
});

// ---------- the combined chart ----------

test('the filter\u2019s targets in one chart: from 2 to 8 of them, from any groups', async ({ page }) => {
  /** @type {string[]} */
  const asked = [];
  page.on('request', (r) => { const u = new URL(r.url()); if (u.pathname.endsWith('/api/panel') && u.searchParams.has('targets')) asked.push(u.searchParams.get('targets') || ''); });
  await page.goto('/#v=c');
  const one = page.getByRole('button', { name: /in one chart$/ });
  await expect(one).toBeHidden();
  await box(page).fill('zoom|github|netflix');
  await expect(one).toHaveText('Show these 3 in one chart');
  await one.click();
  await expect(page).toHaveURL(/cc=1/);
  await expect(page.locator('#panel-targets h2')).toHaveText('Combined chart: 3 targets');
  await expect(charts(page)).toHaveCount(1);
  await expect(page.locator('#panel-targets .u-over')).toBeVisible();
  await expect(legend(page, 'targets')).toHaveText(['Zoom', 'dev-GitHub', 'Netflix']);
  await expect(count(page)).toHaveText('3 targets in one chart');
  expect(asked).toEqual(['Zoom,dev-GitHub,Netflix']);
  // Each line has a colour of its own.
  const colours = await page.locator('#panel-targets .legend .sw').evaluateAll((els) => els.map((e) => getComputedStyle(e).backgroundColor));
  expect(new Set(colours).size).toBe(3);
  // Narrowing the pattern changes the chart; one target left is a chart of its own again.
  await box(page).fill('zoom|netflix');
  await expect(page.locator('#panel-targets h2')).toHaveText('Combined chart: 2 targets');
  await expect(legend(page, 'targets')).toHaveText(['Zoom', 'Netflix']);
  await box(page).fill('zoom');
  await expect(page.locator('#panel-targets')).toBeHidden();
  await expect(page.locator('#panel-call-apps')).toBeVisible();
  // Back to separate charts.
  await box(page).fill('zoom|netflix');
  await expect(page.locator('#panel-targets')).toBeVisible();
  await page.getByRole('button', { name: 'Back to separate charts' }).click();
  await expect(page).not.toHaveURL(/cc=1/);
  await expect(page.locator('#panel-targets')).toBeHidden();
  await expect(charts(page)).toHaveCount(2);
});

test('more than 8 matches are not combined, and the link says why', async ({ page }) => {
  await page.goto('/#v=c&cc=1&oq=' + encodeURIComponent('google|m365|cdn-'));
  await expect(count(page)).toHaveText('12 targets in 3 of 4 charts');
  const one = page.getByRole('button', { name: /in one chart$/ });
  await expect(one).toBeDisabled();
  await expect(page.locator('#cf-many')).toHaveText('One chart shows up to 8 targets: narrow the filter.');
  await expect(page.locator('#panel-targets')).toBeHidden();
  await expect(charts(page)).toHaveCount(3);
  // A link with a combined chart opens on it.
  await page.goto('/#v=c&cc=1&oq=' + encodeURIComponent('^cdn-'));
  await expect(page.locator('#panel-targets h2')).toHaveText('Combined chart: 5 targets');
  await expect(legend(page, 'targets')).toHaveCount(5);
  // Clear filter leaves it.
  await page.getByRole('button', { name: 'Clear filter' }).click();
  await expect(page.locator('#panel-targets')).toBeHidden();
  await expect(charts(page)).toHaveCount(4);
});
