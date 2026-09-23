# FastCAS 账号与管理控制台

使用 React 19、TypeScript 与 Vite。`npm ci && npm run build` 生成 `dist/`；启动 Go 服务时设置 `FASTCAS_UI_DIR=/absolute/path/to/FastCAS/web/dist`，同源提供 `/console/`。未设置时原有 OIDC/JSON API 行为不变。浏览器入口 `/login` 登录成功后返回控制台；OIDC 授权请求仍走原回调。

目前页面包括个人身份复核、认证器 TOTP 开通、项目绑定查看/解除、会话查看/退出；管理员页面包括身份状态、一次性邀请/恢复凭证、应用登记、审计列表和死信重试。管理员 API 要求近期 MFA；页面只展示一次性凭证和客户端密钥，不写入浏览器持久存储。解除关联及改动身份需要再次确认。列表目前读取 API 前 100 项，尚未做完整分页及服务身份维护。

本地开发：`npm run dev -- --port 4173`，Vite 代理 `/api` 与 `/login` 到 `127.0.0.1:8900`；Go 服务需另行启动。邀请注册与密码恢复已提供公开表单：`/console/?flow=invite` 或 `/console/?flow=recovery`。一次性凭证可手工输入，或作为 `token` 查询参数传入；页面初始化会立即清除地址栏查询参数，提交成功后提示用户独立登录。表单仅调用同源 `/api/v1/register`、`/api/v1/recover`，不会自动创建项目账号。生产前需补齐服务身份、跨项目全局退出、管理员/MFA 浏览器验收及可复现部署。

验证：`npm run build` 完成类型检查和打包；Go `TestConsoleRoutingAndSecurityHeaders` 验证静态路由隔离与浏览器安全头。Chrome 对未登录和模拟管理员页面完成 1440px/390px 布局检查，均无横向溢出；模拟响应不能作为实际权限流程通过的证据。

邀请/恢复验证：真实 Go/PostgreSQL HTTP 契约 `TestInviteAndRecoveryHTTPKeepAccountAndInvalidateSession` 通过，覆盖邀请单次消费、恢复保持身份 ID、旧会话失效、旧密码失败与新密码登录。Chrome 模拟 API 的邀请/恢复页面检查确认查询凭证清除、提交一次和成功提示；邀请页 1440px/390px 无横向溢出。浏览器端模拟检查用于布局；真实页面到服务的联调由下述 Chrome 契约覆盖。

真实浏览器闭环：先 `npm ci && npm run build`，再在 FastCAS 根目录运行 `FASTCAS_BROWSER_CONTRACT=1 go test ./internal/httpapi -run '^TestConsoleAgainstRealBrowser$' -count=1 -v`。测试使用隔离 PostgreSQL schema、真实 Go HTTP 服务器、`web/dist` 和 Chrome（可用 `CHROME_BIN` 指定），验证邀请注册、登录进入原身份、会话页、退出、恢复、旧密码拒绝与新密码登录。浏览器视口为 390px；页面无横向溢出。测试生成的数据和一次性凭证均为隔离测试数据。

联调发现登录页的 `Referrer-Policy: no-referrer` 导致 Chromium 原生表单发送 `Origin: null`，与服务端严格同源检查冲突。登录页现在使用 `same-origin`，避免外站收到授权参数，同时保留 Origin/CSRF 拒绝跨站提交。已有跨站登录回归和真实浏览器契约均通过。

应用密钥轮换：管理员完成近期 MFA 后可在“应用管理”选择保密客户端轮换。新密钥只显示一次；旧密钥默认保留 10 分钟，便于项目实例切换，最长可经 API 指定 1 小时。`POST /api/v1/admin/applications/{id}/rotate-secret` 接受可选 `{"grace_seconds": 0..3600}`，0 表示立即失效；未提供时默认 600。连续轮换只保留最近上一把密钥。公开客户端不可轮换。更新与审计在同一事务提交。项目需在交接窗口内更新保密配置，浏览器页面不持久化密钥。
