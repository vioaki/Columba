import { defineConfig, devices } from '@playwright/test'

const port = Number(process.env.PLAYWRIGHT_PORT ?? 5274)
const baseURL = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  use: {
    baseURL,
    channel: process.env.PLAYWRIGHT_CHANNEL,
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
  webServer: process.env.PLAYWRIGHT_EXTERNAL_SERVER ? undefined : {
    command: `npm run dev -- --port ${port} --strictPort`,
    url: baseURL,
    reuseExistingServer: !process.env.CI && !process.env.PROTOCOL_E2E_URL,
  },
})
