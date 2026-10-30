# 本地管理 API

管理路由前缀为 `/api/admin`，仅限回环 Host 和允许的浏览器 Origin。无需面板令牌。响应通常为 `{ok:true,data:...}` 或 `{ok:false,error:{code,message}}`。

| 资源 | 方法和路径 |
| --- | --- |
| 渠道 | GET/POST `/model-sources`；PUT/DELETE `/model-sources/:id`；POST `/model-sources/:id/fetch` |
| 模型 | GET `/models`；POST `/models/refresh`；PATCH/DELETE `/models/:sourceId?modelId=...` |
| 精确价格 | GET/PUT `/model-prices`，按 `sourceId` 和 `modelId` |
| 调度 | GET/PUT `/model-routing`，按 `modelId` 和 `protocol` |
| 调用 Key | GET/POST `/api-tokens`；PUT/DELETE `/api-tokens/:name`；GET `/api-tokens/:name/reveal` |
| 目录 | GET `/model-catalog/status`；POST `/model-catalog/refresh` |
| 费用 | GET `/usage/costs`、`/usage/cost-attempts`，参数见 [计费](billing.md) |
| 用量 | GET `/usage/stats`、`/usage/trend`、`/usage/pulse`、`/usage/by-model`、`/usage/by-model-daily` |
| 明细 | GET `/usage/logs`、`/usage/logs/:id`、`/usage/seq` |
| 留存 | GET `/usage/storage`、`/usage/maintenance`；POST `/usage/cleanup`、`/usage/reset`（保留账目） |
| 设置 | GET/PUT `/runtime-config`；POST `/reload`、`/restart-required/check` |
| 诊断 | GET `/health`、`/logs`、`/migration-report` |

公共 `/health` 是无凭据健康接口。推理与模型发现接口必须传调用 Key：`Authorization: Bearer ...`、Anthropic `x-api-key` 或 Gemini `x-goog-api-key`/`key`。支持 `/v1/models`、`/v1/chat/completions`、`/v1/responses`、`/v1/messages`、`/v1/messages/count_tokens` 和 `/v1beta/models` 下原生生成/流式/countTokens 动作。

旧 `/api/admin/model-groups`、协议编辑、助手、MCP、A2A、自定义 WebSocket 接口均已删除。
