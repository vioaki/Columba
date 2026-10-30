# Columba pricing and accounting

Prices are reference estimates in USD, sourced from the existing models.dev
snapshot/cache/refresh pipeline. Verified against https://models.dev/api.json on
2026-10-30: `cost.input`, `output`, `reasoning`, `cache_read`, `cache_write`, and
`tiers[].tier.{type,size}` with per-tier rates are available. Explicit context
tiers take precedence over the deprecated `context_over_200k` fallback. A tier
applies when reported input context exceeds its size (not at equality).

Each channel/model has an explicit, case-sensitive provider and catalog model ID.
There is no pricing lookup by aliases, shortened IDs or date suffixes. Manual
rates override catalog values (including matching fields on catalog tiers), and
manual tiers replace catalog tiers. Blank means unavailable or inherited; zero
is a known zero rate. Saving prices does not alter the catalog, and refreshing the
catalog does not change manual settings. The panel shows the catalog timestamp.

Prices use decimal strings and exact arithmetic. Tool calls, image/audio and
per-second fees are outside the first version's scope. Anthropic one-hour cache
writes require a separate manual price; the catalog's generic cache-write price
is not silently reused for that duration. Historical usage is never repriced.

The log database has a separate `billing_attempts` ledger without foreign keys
into diagnostic records. Usage cleanup/reset never deletes it. Before contacting
an upstream, Columba stores a pending row and freezes its price snapshot. A
storage failure at this point prevents the upstream request. Settlement persists
reported counters and the calculated amount synchronously, even for failed
attempts; an interrupted process or failed settlement remains visibly pending.
The request ID and attempt number are unique, and a settled row cannot be
settled again. USD amounts are rounded once per attempt to 12 decimal places;
summary amounts are added with exact decimal arithmetic, never SQLite REAL.

`GET /api/admin/usage/costs` returns request/attempt totals, incomplete attempt
counts, UTC daily trends, and model/channel/calling-Key breakdowns.
`GET /api/admin/usage/cost-attempts` returns 100 records per page with snapshots.
Both accept exact `model`, `source`, `key`, `request`, and millisecond `from` /
exclusive `to` filters; the detail endpoint also accepts `offset`.

Latest legacy usage migrates as `historical_unpriced` (attempt zero), retaining
its reported counters but no amount. Missing usage, missing prices, partial
pricing, pending settlement and known zero costs remain separate states.

Observed built-in tool usage marks token costs partial. Reported audio/video or
image-output token usage is not priced at text rates: counters remain available,
but the amount is unavailable until those modality-specific prices are supported.
Interrupted responses retain reported counters and flag possible incomplete usage.

Usage charts normalize Anthropic cache input and Gemini reasoning output into
inclusive input/output totals. A missing total is summed only from both reported
components, never from character estimates. Ledger counters retain their original
protocol basis for pricing and auditing.
