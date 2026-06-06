const { defineConfig } = require('@playwright/test');

const baseURL = process.env.PODOREL_E2E_UI_URL || 'http://localhost:14200';

module.exports = defineConfig({
  testDir: './e2e',
  timeout: 90_000,
  expect: {
    timeout: 12_000
  },
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL,
    acceptDownloads: true,
    actionTimeout: 15_000,
    navigationTimeout: 30_000,
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'retain-on-failure'
  },
  webServer: process.env.PODOREL_E2E_SKIP_SERVER
    ? undefined
    : {
        command: 'node e2e/dev-server.mjs',
        url: baseURL,
        reuseExistingServer: false,
        timeout: 180_000,
        gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
        stdout: 'pipe',
        stderr: 'pipe'
      }
});
