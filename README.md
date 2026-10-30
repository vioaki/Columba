# Columba

<img src="packages/webui/brand/Columba.svg" width="64" alt="Columba">

本地优先的 LLM 网关。模型聚合 · 原生透传 · 渠道调度 · 用量计费。

Go · React · SQLite

## 功能

- **原生协议**：OpenAI Chat Completions / Responses、Anthropic Messages、Gemini。
- **模型聚合**：同协议、同 ID 自动聚合，启用即对外可用。
- **渠道调度**：轮询、优先级、加权，支持多 Key 与失败重试。
- **用量计费**：[models.dev](https://models.dev) 价格目录与手动定价，按模型、渠道、调用 Key 统计。

支持 macOS App，以及 Windows / Linux / macOS 单文件运行。面板免登录，API 调用使用 Key，服务仅监听本机。

## 使用

1. 启动 `Columba.app` 或对应平台的 `columba-*` 可执行文件，打开 [本地面板](http://127.0.0.1:8765/ui/)。
2. 在「模型」添加渠道和上游凭据，拉取或手动添加模型并启用。
3. 在「调用 Key」创建密钥，客户端使用原始模型 ID 发起调用。

配置参考 [config.json.example](config.json.example)，通过 `-config /path/config.json` 指定。连接本地模型服务时，需在设置中放行对应的私网地址。

费用以 USD 估算，覆盖 Token 与缓存；缺失价格或用量会单独标记。

## 构建

需要 Go 1.27.2+、Node.js 22+；macOS App 额外需要 Xcode 或兼容的 Command Line Tools。

```sh
npm ci
npm run build            # Windows / Linux / macOS，amd64 + arm64
npm run build:macos-app  # macOS Universal App + DMG
```

产物位于 `dist/standalone/`。

## 检查

```sh
npm run lint
npx playwright install chromium webkit  # 首次安装测试浏览器
npm run test:webui
```

后端检查在 `backend/` 目录执行：

```sh
go test -race ./...
go vet ./...
```
