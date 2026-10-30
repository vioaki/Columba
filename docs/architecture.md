# Columba 架构

- `backend/server`：本地管理接口、调用 Key 校验、模型发现、原生 HTTP 转运、诊断与费用接口。
- `backend/internal/routing`：精确模型 ID/协议候选与渠道/Key 调度。
- `backend/internal/nativeusage`：有界 JSON/SSE 用量旁路观察；失败不影响正文。
- `backend/internal/billing`：models.dev 价格结构、Token 口径归一化与精确 USD 计算。
- `backend/storage`：业务/日志双 SQLite、迁移、独立尝试账本和续接归属。
- `packages/webui`：五项导航的本地 React 面板。
- `scripts/macos-app`：系统框架实现的原生窗口、托盘及后端进程管理。关闭面板释放 UI/WebKit，保留托盘与 API 服务；退出应用停止其拥有的后端。

推理请求先通过调用 Key 校验，再按精确 ID 和原生协议选出启用渠道。发送前记录待结算账目与价格快照；HTTP 层只处理地址、认证和传输头。正文和 SSE 事件保持原样，旁路读取用量并逐尝试结算。只有明确可重试状态且未输出时才能换渠道。续接状态保存原渠道、凭据和地址，未知归属返回冲突错误。

管理接口没有用户、登录态或面板令牌。服务仅绑定回环地址，管理路由校验 Host/Origin。费用记录独立于可清理的诊断正文，历史金额不重算。
