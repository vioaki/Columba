# Columba WebUI

本地免登录管理面板，使用 React、TypeScript、Vite 与 Tailwind。主导航为总览、模型、调用 Key、用量费用、设置。调用明细归入用量费用，系统日志与诊断归入设置。黑白灰主题支持浅色、深色与键盘操作。

```sh
npm ci
npm run dev --workspace @root/webui
npm run lint --workspace @root/webui
npm run build:webui
npm run test:e2e --workspace @root/webui
```

开发服务默认 `http://127.0.0.1:5273`，代理到本机 `8765` 后端；可用 `COLUMBA_DEV_PROXY` 指定后端地址。管理请求通过明确允许的本地来源访问，推理客户端仍需调用 Key。
