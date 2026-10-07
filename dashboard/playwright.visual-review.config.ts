import { defineConfig, devices } from 'playwright/test'

// Visual review is deliberately separate from the assertion-based E2E suite. Its screenshots are
// disposable evidence for direct inspection, not baselines or pass/fail pixel comparisons.
export default defineConfig({
  testDir: './tests/visual-review',
  fullyParallel: true,
  workers: 4,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  outputDir: 'test-results/visual-review',
  preserveOutput: 'always',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: true,
  },
  projects: [
    {
      name: 'desktop-dark',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, colorScheme: 'dark' },
    },
    {
      name: 'mobile-dark',
      use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 }, colorScheme: 'dark' },
    },
    {
      name: 'desktop-light',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, colorScheme: 'light' },
    },
    {
      name: 'mobile-light',
      use: { ...devices['Desktop Chrome'], viewport: { width: 390, height: 844 }, colorScheme: 'light' },
    },
  ],
})
