<h1><img src="packages/webui/brand/Columba.svg" width="40" height="40" align="top" alt=""> Columba</h1>

本地优先的 LLM 网关。模型聚合 · 原生透传 · 渠道调度 · 用量计费。

Go · React · SQLite

## 功能

- **原生协议**：OpenAI Chat Completions / Responses、Anthropic Messages、Gemini。
- **模型聚合**：同协议、同 ID 自动聚合，启用即对外可用。
- **渠道调度**：轮询、优先级、加权，支持多 Key 与失败重试。
- **用量计费**：[models.dev](https://models.dev) 价格目录与手动定价，按模型、渠道、调用 Key 统计。

支持 macOS App，以及 Windows / Linux / macOS 单文件运行。面板使用管理密码登录，API 调用使用独立 Key，默认仅监听本机。

## 使用

1. 启动 `Columba.app` 或对应平台的 `columba-*` 可执行文件，打开 [本地面板](http://127.0.0.1:8765/ui/)。
2. 首次在本机设置管理密码，登录后在「模型」添加渠道和模型。
3. 启用模型，在「调用 Key」创建密钥，客户端使用原始模型 ID 发起调用。

配置参考 [config.json.example](config.json.example)，通过 `-config /path/config.json` 指定。连接本地模型服务时，需在设置中放行对应的私网地址。

管理密码可在「设置 → 登录安全」修改。会话有效期为 24 小时，重启服务后需重新登录。忘记密码时，在本机用相同配置加上 `-reset-panel-password` 参数运行可执行文件即可重置。

费用以 USD 估算，覆盖 Token 与缓存；缺失价格或用量会单独标记。

### 服务器

由 systemd 等服务管理器运行，关闭 `openBrowserOnStart`。局域网访问需明确配置监听地址和面板来源：

```json
{
  "host": "192.168.5.48",
  "port": 8765,
  "panelOrigins": ["http://192.168.5.48:8765"],
  "openBrowserOnStart": false
}
```

首次设置管理密码需通过服务器本机或 SSH 隧道完成，再开放网络监听。公网访问应通过 HTTPS 反向代理，并将实际面板来源加入 `panelOrigins`。

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
