import { defineConfig, devices } from '@playwright/test';

/**
 * See https://playwright.dev/docs/test-configuration.
 *
 * BASE_URL env var selects the target. Defaults to the local Next.js dev
 * server on :3003 (matching the local-run convention). CI or K8s smoke
 * tests can override with BASE_URL=http://localhost:8080 or similar.
 */
const BASE_URL = process.env.BASE_URL || 'http://localhost:3003';

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL: BASE_URL,
    trace: 'on-first-retry',
    viewport: { width: 1280, height: 720 },
  },
  // Only auto-start the dev server when pointing at the local default.
  // If the user sets BASE_URL (e.g. pointing at a K8s ingress) we assume
  // they already have something running there.
  webServer: process.env.BASE_URL ? undefined : {
    command: 'npx next dev -p 3003',
    url: 'http://localhost:3003',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'mobile-chrome',
      use: { ...devices['Pixel 5'] },
    },
  ],
});
