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

test('a pattern that takes too long is stopped', async ({ page }) => {
  // A matcher that never answers stands in for a pattern with runaway backtracking.
  await page.route('**/static/match.js', (r) => r.fulfill({ contentType: 'text/javascript', body: 'self.onmessage = () => {};' }));
  await page.goto('/#v=c');
  await expect(charts(page)).toHaveCount(4);
  await box(page).fill('zoom');
  await expect(page.locator('#cf-note')).toContainText('takes too long');
  await expect(charts(page)).toHaveCount(4);
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
