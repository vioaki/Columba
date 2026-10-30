# Columba

<img src="packages/webui/brand/Columba.svg" width="72" alt="Columba">

A local model gateway for aggregation, native protocol passthrough, channel scheduling, and usage/cost accounting. Go + React + SQLite, with a native macOS App and standalone Windows/Linux/macOS builds.

The management panel requires no login and listens only on loopback. Model calls require an API Key. Add a channel and enable its models, create a calling Key, then send the original model ID. Channels aggregate by exact model ID and protocol: Chat Completions, Responses, Anthropic Messages, and Gemini. No group aliases or protocol translation. Scheduling supports round robin, priority and weights; defaults allow three extra attempts, one second apart. Output, cancellation and uncertain transport failures stop retries.

Run a platform binary in a dedicated directory, or open Columba.app. Visit `http://127.0.0.1:8765/ui/`. Use `-config /path/config.json` for explicit configuration. macOS App data lives in `~/Library/Application Support/Columba`. Default outbound rules block private networks; adjust `outbound.deniedIpRanges` explicitly for local upstreams. See [config.json.example](config.json.example).

Pricing uses explicit provider/model mappings from models.dev and manual decimal overrides. USD estimates cover reported tokens/cache only. Missing prices/usage remain incomplete, not free. Tool, image, audio and per-second fees are outside this version's scope. Every attempt freezes prices; log cleanup preserves the independent ledger.

Only legacy business schema 2 / log schema 1 upgrades automatically to current 6 / 3. Upgrades use paired backups including committed WAL, recover interrupted migrations, and reject unknown formats. No historical directory discovery. Old usage remains unpriced; unsupported custom channels are retained but disabled. Clients must replace group names with original model IDs.

```sh
npm ci
npm run build
npm run build:macos-app     # macOS only: Universal App + DMG
npm run test:webui         # temporary test databases; Chromium + WebKit
npm run test:standalone
```

See [development](docs/development.md), [native gateway](docs/native-gateway.md), [billing](docs/billing.md), [storage](docs/storage-schema.md), and [macOS checks](docs/macos-testing.md). Builds do not publish, push, auto-update, or increment the product version.
