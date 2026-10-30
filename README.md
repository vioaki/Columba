# Columba

<img src="packages/webui/public/Columba.svg" width="72" alt="Columba">

Columba 是本地模型网关：聚合模型、透传原生协议、调度渠道，并记录用量与参考费用。Go 后端、React 面板和双 SQLite 数据库；提供 macOS 原生 App，以及 Windows/Linux/macOS 单文件构建。

面板免登录，服务只监听回环地址；客户端推理仍需调用 Key。导航只有总览、模型、调用 Key、用量费用、设置。

## 使用

1. 启动 Columba.app，或在专用目录运行本机平台的 `columba-*` 可执行文件。
2. 打开 `http://127.0.0.1:8765/ui/`，在“模型”添加渠道、原生协议、上游地址与凭据，拉取或手动添加模型。
3. 启用渠道与模型，在“调用 Key”创建客户端凭据。客户端使用原始模型 ID，不再使用组名。
4. 在模型价格设置中明确填写 models.dev 供应商/模型 ID，或手动填写价格。“用量费用”查看 USD 费用、未计价数量和调用明细。

支持 Chat Completions、Responses、Anthropic Messages、Gemini 原生调用。同名模型按**精确 ID＋协议**聚合；无别名推测或跨协议转换。调度支持轮询、优先级和加权；默认额外重试最多 3 次、间隔 1 秒。已经输出、客户端取消或发送结果不确定时不会重试。

配置参考 [config.json.example](config.json.example)。默认出站规则禁止私网目标；连接本地模型服务时，可在设置中明确调整 `outbound.deniedIpRanges`。出站代理通过 `outbound.proxyUrl` 设置。

## 数据与升级

macOS App 使用 `~/Library/Application Support/Columba`。命令行配置默认位于工作目录，数据库路径相对于配置文件。使用 `-config /path/config.json` 指定配置。密钥由 `secretKeyPath` 或 `COLUMBA_MASTER_KEY` 提供。

只支持最新旧结构（业务库 2 / 日志库 1）自动升级至当前结构（业务库 6 / 日志库 3），不搜索旧数据目录。首次升级会创建包含已提交 WAL 的成套备份；中断恢复和未知格式拒绝见 [存储说明](docs/storage-schema.md)。旧用量标记为历史未计价。自定义渠道无法可靠转换时保留连接资料并停用；旧组名不提供兼容层。

费用是 USD 参考估算，覆盖已报告的 Token 和缓存。缺少用量或价格不会显示成免费；工具、图片、音频、按秒等费用不在本版覆盖范围。账目独立于诊断正文留存，清理日志不会删除账目。历史价格快照不会随目录更新重算。

## 开发与构建

```sh
npm ci
npm run build                 # 六个平台/架构二进制，包含 WebUI
npm run build:macos-app       # 仅 macOS：Universal App 与 DMG
npm run test:webui            # 生成临时双库，Chromium + WebKit
npm run test:standalone       # 本机二进制、透传、计费和内嵌界面冒烟
```

详细说明：[开发](docs/development.md) · [macOS 检查](docs/macos-testing.md) · [模型调度](docs/model-routing.md) · [原生网关](docs/native-gateway.md) · [计费](docs/billing.md) · [管理 API](docs/webui-api.md)

构建不会推送、发布或自动更新应用。产品版本由现有包版本/标签决定，不由构建脚本递增。
