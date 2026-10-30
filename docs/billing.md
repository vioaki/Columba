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
