// Playwright config for the fyisp UI tests. The Dockerfile's uitest stage
// starts fyisp-webdev on 127.0.0.1:3000 and runs `npx playwright test` here.
// @ts-check
const { defineConfig, devices } = require('@playwright/test');

// These specs change the shared fyisp-webdev's state (speed tests and the
// schedule, on-demand traces), so they run one at a time, after the others.
// The rest only read it, serve api/home from fixtures, or start their own
// fyisp-webdev on a port no other test uses.
const STATEFUL = ['speed', 'speed-pause', 'investigate', 'investigate-host'].map((n) => `**/${n}.spec.js`);

module.exports = defineConfig({
  testDir: './specs',
  outputDir: './out/results',
  forbidOnly: true,
  retries: 0,
  // A fixed number, so a run is the same on a laptop and on CI's 4 cores.
  workers: 4,
  reporter: [['list'], ['html', { outputFolder: './out/report', open: 'never' }]],
  use: {
    ...devices['Desktop Chrome'],
    viewport: { width: 1400, height: 900 },
    baseURL: process.env.FYISP_URL || 'http://127.0.0.1:3000',
    // The Home fixtures' copy is written for 10:30 am on 2026-10-09 in New York.
    timezoneId: 'America/New_York',
    locale: 'en-US',
    // fyisp-webdev's fixed admin token, on every request, as a script would
    // send it; login.spec.js signs in without it.
    extraHTTPHeaders: { 'X-FYISP-Admin': 'fyisp-webdev-not-a-secret' },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', testIgnore: STATEFUL, fullyParallel: true },
    // Not run at all when a desktop test fails; --no-deps runs it alone.
    { name: 'stateful', testMatch: STATEFUL, dependencies: ['desktop'], workers: 1 },
  ],
});
