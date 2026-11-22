// Investigate traces any public host typed in its host box, with the
// picked probe type. The trace is named "host:<host>": the URL reopens it,
// and the mtr view, the report and "Run it yourself" work as for a target.
// A refusal is shown under the box with the server's reason. A pasted
// host: link never starts a trace by itself: Trace does.
// @ts-check
const { test, expect } = require('./fixtures');

test('the host box traces a public host, and the URL reopens it', async ({ page }) => {
  /** @type {string[]} */
  const posts = [];
  // The answer is fixed: other specs may hold every slot of fyisp-webdev.
  await page.route('**/api/investigate?*', (route) => {
    posts.push(new URL(route.request().url()).search);
    return route.fulfill({ json: { target: 'host:example.com', mode: 'tcp', state: 'running', rounds_done: 0, rounds_max: 250, interval_ms: 2500, started_at: Date.now() } });
  });
  await page.goto('/#' + new URLSearchParams({ t: 'Google DNS 8.8.8.8', r: '1h' }));
  const inv = page.locator('#inv');
  await inv.locator('.inv-modes').getByRole('button', { name: 'TCP', exact: true }).click();
  const box = inv.getByRole('textbox', { name: 'Host to trace' });
  await box.fill('Example.com');
  await box.press('Enter');
  await expect(inv.locator('.inv-status .pill.on', { hasText: /^Tracing… \d+ of 250 rounds$/ })).toBeVisible();
  // Switching the target to TCP 443 starts its own trace first.
  expect(posts).toEqual(['?target=Google+DNS+8.8.8.8&mode=tcp&rounds=200', '?host=Example.com&mode=tcp&rounds=200&port=443']);
  await expect(page).toHaveURL(/#.*t=host%3Aexample\.com/);
  await expect(inv.locator('.inv-pick')).toHaveValue('host:example.com');
  await expect(inv.locator('.inv-pick option:checked')).toHaveText('example.com');
  await expect(inv.locator('.inv-run .inv-cmd')).toHaveText('mtr -T -P 443 -rwzbc 100 example.com');
  await expect(inv.locator('.inv-host-err')).toBeEmpty();

  // Reopened from the URL: not started by itself.
  await page.goto('/#' + new URLSearchParams({ t: 'host:example.com' }));
  await expect(inv.locator('.inv-status .pill', { hasText: 'Not tracing' })).toBeVisible();
  await expect(box).toHaveValue('example.com');
  await expect(inv.locator('.inv-run .inv-cmd')).toHaveText('mtr -rwzbc 100 example.com');
  expect(posts).toHaveLength(2);
});

test('a pasted host: link never starts a trace by itself; Trace does', async ({ page }) => {
  /** @type {string[]} */
  const posts = [];
  await page.route('**/api/investigate?*', (route) => {
    if (route.request().method() === 'POST') posts.push(new URL(route.request().url()).search);
    return route.fulfill({ json: { target: 'host:example.com', mode: 'icmp', state: 'running', rounds_done: 0, rounds_max: 250, interval_ms: 2500, started_at: Date.now() } });
  });
  await page.goto('/#' + new URLSearchParams({ t: 'host:example.com' }));
  const st = page.locator('#inv .inv-status');
  await expect(st.locator('.pill', { hasText: 'Not tracing' })).toBeVisible();
  await expect(page.locator('#inv .inv-summary')).toContainText('Not tracing. Click Trace');
  await page.waitForLoadState('networkidle');
  expect(posts).toEqual([]);

  await st.getByRole('button', { name: 'Trace', exact: true }).click();
  await expect(st.locator('.pill.on', { hasText: /^Tracing… \d+ of 250 rounds$/ })).toBeVisible();
  expect(posts).toEqual(['?target=host%3Aexample.com&mode=icmp&rounds=200']);
});

test('a refused host shows the server\'s reason under the box', async ({ page, failOk }) => {
  failOk.push(/\/api\/investigate\b/);
  await page.goto('/#' + new URLSearchParams({ t: 'Google DNS 8.8.8.8', r: '1h' }));
  const inv = page.locator('#inv');
  const box = inv.getByRole('textbox', { name: 'Host to trace' });
  const err = inv.locator('.inv-host-err');

  // Checked by fyisp-webdev itself (syntax comes before the limits).
  await box.fill('not a host');
  await inv.getByRole('button', { name: 'Trace host' }).click();
  await expect(err).toHaveText('Can’t trace this host: not a host name or IPv4 address.');
  await expect(box).toHaveAttribute('aria-invalid', 'true');
  await expect(page).not.toHaveURL(/host%3A/);

  // A name on the LAN (answered here: the slots may be taken by other specs).
  await page.route('**/api/investigate?*', (route) => route.fulfill({ status: 403,
    json: { code: 'not_public', error: 'nas.lan resolves to a private, local or otherwise non-public address: only public hosts can be traced' } }));
  await box.fill('nas.lan');
  await box.press('Enter');
  await expect(err).toHaveText('Can’t trace this host: nas.lan resolves to a private, local or otherwise non-public address: only public hosts can be traced.');
  await expect(inv.locator('.inv-pick')).toHaveValue('Google DNS 8.8.8.8');
});

test('the TCP port box traces the host to another port as its own trace', async ({ page }) => {
  /** @type {string[]} */
  const posts = [];
  await page.route('**/api/investigate?*', (route) => {
    const u = new URL(route.request().url());
    posts.push(u.search);
    const port = u.searchParams.get('port');
    const target = 'host:8.8.8.8' + (port && port !== '443' ? `:${port}` : '');
    return route.fulfill({ json: { target, mode: 'tcp', state: 'running', rounds_done: 0, rounds_max: 200, interval_ms: 2500, started_at: Date.now() } });
  });
  await page.goto('/#' + new URLSearchParams({ t: 'Google DNS 8.8.8.8', r: '1h' }));
  const inv = page.locator('#inv');
  const port = inv.getByRole('textbox', { name: 'TCP port' });
  await expect(port).toBeHidden(); // ICMP
  await inv.locator('.inv-modes').getByRole('button', { name: 'TCP', exact: true }).click();
  await expect(port).toHaveValue('443');
  await expect(inv.locator('.inv-mode-note')).toContainText('TCP SYNs to port 443');

  // Not a port: refused in the page, nothing sent.
  const n = posts.length;
  await port.fill('0');
  await port.press('Enter');
  await expect(inv.locator('.inv-host-err')).toHaveText('The port must be a whole number from 1 to 65535.');
  expect(posts.length).toBe(n);

  await port.fill('2222');
  await port.press('Enter');
  await expect.poll(() => posts.at(-1)).toBe('?host=8.8.8.8&mode=tcp&rounds=200&port=2222');
  await expect(page).toHaveURL(/#.*t=host%3A8\.8\.8\.8%3A2222/);
  await expect(port).toHaveValue('2222');
  await expect(inv.locator('.inv-mode-note')).toContainText('TCP SYNs to port 2222');
  await expect(inv.locator('.inv-run .inv-cmd')).toHaveText('mtr -T -P 2222 -rwzbc 100 8.8.8.8');

  // Back to 443: the profile target it came from, not a new trace of its host.
  const before = posts.length;
  await port.fill('443');
  await port.press('Enter');
  await expect(page).toHaveURL(/#.*t=Google\+DNS\+8\.8\.8\.8/);
  await expect(port).toHaveValue('443');
  expect(posts.slice(before).every((q) => !q.startsWith('?host='))).toBe(true);
  await port.fill('2222');
  await port.press('Enter');
  await expect(page).toHaveURL(/#.*t=host%3A8\.8\.8\.8%3A2222/);

  // Reopened from the URL, it keeps its port; ICMP goes back to the host itself.
  await page.reload();
  await expect(port).toHaveValue('2222');
  await inv.locator('.inv-modes').getByRole('button', { name: 'ICMP', exact: true }).click();
  await expect(page).toHaveURL(/#.*t=host%3A8\.8\.8\.8(&|$)/);
  await expect(port).toBeHidden();
});


test('a refused port change leaves the shown trace and its port alone; arrows do not step the port', async ({ page, failOk }) => {
  failOk.push(/\/api\/investigate\b/);
  await page.route('**/api/investigate?*', (route) => {
    const u = new URL(route.request().url());
    if (u.searchParams.get('port') === '22') return route.fulfill({ status: 429, headers: { 'Retry-After': '30' }, json: { code: 'busy', error: 'too many on-demand traces running', retry_after: 30 } });
    return route.fulfill({ json: { target: 'Google DNS 8.8.8.8', mode: 'tcp', state: 'running', rounds_done: 0, rounds_max: 200, interval_ms: 2500, started_at: Date.now() } });
  });
  await page.goto('/#' + new URLSearchParams({ t: 'Google DNS 8.8.8.8', r: '1h', im: 'tcp' }));
  const inv = page.locator('#inv');
  const port = inv.getByRole('textbox', { name: 'TCP port' });
  await expect(port).toHaveValue('443');
  await port.press('ArrowUp');
  await expect(port).toHaveValue('443');
  await port.fill('22');
  await port.press('Enter');
  await expect(inv.locator('.inv-host-err')).not.toBeEmpty();
  await expect(page).toHaveURL(/#.*t=Google\+DNS\+8\.8\.8\.8/);
  await page.waitForTimeout(6000); // a refresh: still 443's note and command
  await expect(inv.locator('.inv-mode-note')).toContainText('TCP SYNs to port 443');
  await expect(inv.locator('.inv-run .inv-cmd')).toHaveText('mtr -T -P 443 -rwzbc 100 8.8.8.8');
  // A bad port: said under the box, and the port box is described by it; gone after leaving TCP.
  await port.fill('0');
  await port.press('Enter');
  await expect(port).toHaveAttribute('aria-invalid', 'true');
  await expect(port).toHaveAccessibleDescription(/whole number from 1 to 65535/);
  await inv.locator('.inv-modes').getByRole('button', { name: 'ICMP', exact: true }).click();
  await expect(inv.locator('.inv-host-err')).toBeEmpty();
});
