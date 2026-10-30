# macOS 构建与测试

```sh
npm run test:macos-app
npm run build
npm run build:macos-app
npm run test:macos-app -- --panel
```

原生测试使用临时 App、独立 bundle ID、临时配置和数据库，不访问 `~/Library/Application Support/Columba`。基础测试使用本地 fixture，检查配置保留、窗口、主题、后台生命周期、下载取消/导出及托盘行为。`--flows` 只运行托盘流程。`--panel` 使用刚构建的真实内嵌后端和 WebUI，并保存 `dist/macos-panel-preview.png`。

托盘测试检查重复启动复用同一服务，关窗后 UI/WebKit 进程退出，API 保持健康，主进程崩溃后子进程退出，以及普通退出释放资源。WebKit 对 localStorage 批量落盘，合成偏好计数测试在关闭前等待落盘；真实主题另由原生偏好保存和恢复。

构建要求能链接 macOS 12 上 arm64 与 x86_64 的 Swift 工具链。先运行 `npm run build:macos-app -- --check-toolchain` 检查。若当前 Command Line Tools 缺少某架构运行库，用 `DEVELOPER_DIR` 指向已安装的兼容 Xcode。构建不会自行下载工具链。

图标统一从 `packages/webui/public/Columba.svg` 生成。产物为 Universal `Columba.app` 和 `columba-macos.dmg`，本地 ad-hoc 签名，未公证。构建校验架构、plist、签名和挂载后的 DMG；不自动替换已安装应用，也不发布。
