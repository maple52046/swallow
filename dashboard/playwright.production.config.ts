import { defineConfig, devices } from 'playwright/test'

// Release-build smoke checks. Unlike playwright.config.ts (the Vite dev server), this serves the real
// `vite build` output through `vite preview`, so it proves what operators get: in-development
// features hidden and no experimental-features switch. API calls are still answered by the
// deterministic fixtures in tests/e2e/fixtures.ts.
export default defineConfig({
  testDir: './tests/production',
  fullyParallel: true,
  workers: 2,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL: 'http://127.0.0.1:4174',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run build && npx vite preview --host 127.0.0.1 --port 4174 --strictPort',
    url: 'http://127.0.0.1:4174',
    // Always rebuild: a reused server could be serving a stale or development bundle.
    reuseExistingServer: false,
    timeout: 240_000,
  },
  projects: [{
    name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, colorScheme: 'light' },
  }],
})
