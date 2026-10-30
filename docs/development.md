# 开发与验证

使用 backend/go.mod 指定的 Go 版本与 Node.js 22.13+（浏览器测试使用 node:sqlite）。在仓库根目录执行 `npm ci`，并运行 `npx playwright install chromium webkit` 安装测试浏览器。

开发后端时使用显式配置路径和专用测试目录；不要对个人历史数据库运行测试。前端启动命令为 `npm run dev --workspace @root/webui`，默认仅在 `127.0.0.1:5273` 服务。`COLUMBA_DEV_PROXY` 可指定本机后端。管理接口明确允许同源及 5273/5274 的本地开发来源，不支持公网或跨站面板。

```sh
cd backend
go test ./...
go vet ./...
go test -race ./internal/routing ./internal/nativeusage ./internal/billing ./storage ./server
cd ..
npm run lint --workspace @root/webui
npm run build:webui
npm run test:webui
npm run build
npm run test:standalone
```

`test:webui` 启动临时后端和 Vite，对两个浏览器运行全部测试，记录生成的数据库目录供排查。可传文件名缩小范围。测试运行器只停止自己创建的进程。常规 `npm run test:e2e --workspace @root/webui` 使用模拟接口，跳过需要真实后端的用例。

`npm run build` 生成 Windows/Linux/macOS × amd64/arm64 的六个二进制，内嵌 WebUI，无运行期 Node 依赖。输出在 `dist/standalone/`。`test:standalone` 运行本机目标，验证无登录面板、来源限制、调用 Key、原生正文、费用账目和内嵌资源。

`node scripts/update-model-catalog.mjs` 更新内嵌 models.dev 能力与价格快照，可传 `--proxy` 或 `--url`。价格必须精确映射；人工价格不随目录刷新覆盖。

提交按单一职责拆分，检查通过后提交。CI 只校验和保存构建产物，不创建发布、不上传镜像。
