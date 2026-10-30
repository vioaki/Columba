# Columba WebUI

Local, login-free React/TypeScript management panel. Five sections: overview, models, calling Keys, usage/costs, and settings. Call details live under usage/costs; system logs and diagnostics live under settings. Neutral light/dark themes support keyboard navigation and small windows.

Run `npm ci` at the repository root, then `npm run dev --workspace @root/webui`. The local Vite server uses port 5273 and proxies to port 8765 (`COLUMBA_DEV_PROXY` overrides the backend). Run the workspace lint, `npm run build:webui`, and `npm run test:e2e --workspace @root/webui` to check changes. Model calls still require an API Key.
