# Columba native gateway

The gateway aggregates enabled channels by exact model ID and native protocol
(`openai`, `responses`, `claude`, `gemini`). Request and response bodies are
forwarded unchanged, including unknown fields and SSE events. Usage observation
is bounded and independent of delivery; missing or malformed usage is reported.

Only 429, 500, 502, 503 and 504 responses may advance to another channel/key,
before downstream output. Transport uncertainty, cancellation and interrupted
output stop retries. Stateful continuations require persisted ownership of the
channel, credential and base URL, scoped to the calling API key.

Public endpoints use this native transport. Model groups and protocol editors
are retired; clients must send the original model ID. Model scheduling is stored
per exact model ID and protocol; default retries are three extra attempts with
a one-second interval. Disabled/deleted channels are excluded immediately.

Coverage:
four ordinary/SSE protocols, raw bytes, failed-attempt usage, retries, interrupted
output, cancellation and continuation ownership. Generation-based health checks
and cross-protocol translation are not part of the native gateway.
