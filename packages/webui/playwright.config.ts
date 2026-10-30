import { defineConfig, devices } from '@playwright/test'

const baseURL = 'http://127.0.0.1:5274'

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  use: {
    baseURL,
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
  webServer: {
    command: 'npm run dev -- --port 5274 --strictPort',
    url: baseURL,
    reuseExistingServer: false,
  },
})
