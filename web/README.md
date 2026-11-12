# FastCAS 账号与管理控制台

React 19、TypeScript、Vite；使用 npm 正式包 `@fastrd/ui-react@0.1.2` 和 `@fastrd/ui-foundation@0.1.2`，版本与完整性由 `pnpm-lock.yaml` 固定。共享控件、反馈、对话框及颜色令牌来自 FastUI，应用布局与身份业务留在 FastCAS。界面与验收面向桌面管理场景。

```sh
pnpm install --frozen-lockfile
pnpm run build
```

构建生成 React 控制台和原生认证页使用的独立入口。设置 `FASTCAS_UI_DIR=/absolute/path/to/FastCAS/web/dist` 后，由 Go 同源提供 `/console/`，登录后返回控制台；OIDC 请求继续使用原授权回调。部署时必须复制整个 `dist/`，包括 `.vite/manifest.json` 和所有 `assets/`。服务启动时验证认证入口及其依赖，缺少产物会报错并要求重新构建。manifest 不向浏览器开放。Docker 和本地发布脚本已复制整个目录。

未设置 `FASTCAS_UI_DIR` 时，原生登录、同意与设备确认仍可使用，React 控制台不挂载。已启用控制台时，这些原生页面也使用同版本 npm 包构建的样式与主题；登录和设备确认继续由服务端处理，表单不依赖 JavaScript，Origin/CSRF 与授权状态检查保持在服务端。

个人页面包括身份复核、MFA 开通与恢复码轮换、自助改密、项目关联、设备安装、跨项目委托和会话撤销。管理页面包括身份状态、邀请/密码恢复/MFA 重置、应用、服务身份、调用策略、接入指引、审计和失败投递。身份、应用、服务身份、审计及设备列表支持服务端分页。管理员 API 要求近期 MFA；高风险操作使用 FastUI 原生模态对话框确认。应用权限和通知地址通过可校验表单编辑，失败时保留输入并在对话框内显示错误。

审计支持操作名称、执行者 ID、目标 ID 的精确匹配和开始/结束时间组合筛选；分页保留已应用条件，重置与刷新从首屏重新查询。输入时间使用浏览器时区，发送时转为 UTC；开始时间包含、结束时间排除。后台迁移 `015_audit_filters.sql` 为检索与分页增加索引。

主题默认使用深色，支持浅色、深色和跟随系统，使用 foundation 的颜色模式控制器；仅 `fastcas.color-mode` 偏好写入 localStorage。同源经典脚本 `assets/theme-init.js` 在 head 阻塞初始化首屏主题，Shell 和原生认证页复用同一 controller，系统或跨标签主题变化保持表单 DOM、草稿和焦点。一次性凭证、客户端密钥、恢复码和认证器密钥只保存在当前页面内存中；离开对应页面会卸载组件并清空显示，管理页之间不会复用密钥。查询参数中的邀请/恢复凭证在 React 发出请求前从地址栏清除。凭证须通过可信渠道私密交付。

未登录访问 `/`、`/console` 或 `/console/` 会直接跳转 `/login`。登录页提供登录和注册标签，注册标签保留邀请注册、密码恢复、双重验证重置三种流程。公开表单入口为 `/login?flow=invite`、`?flow=recovery`、`?flow=mfa_reset`，支持手动凭证或 `token` 查询参数；原 `/console/?flow=…` 链接继续转到对应登录页流程。认证器设置使用本地生成的二维码，同时保留手动导入 URI。切换流程清空原凭证与密码。成功后提示独立登录，各项目账号不会自动创建或合并。

本地开发：`pnpm run dev -- --port 4173`。Vite 将 `/api`、`/login`、`/device` 与 discovery 代理到 `127.0.0.1:8900`。原生认证页的完整样式与主题联调应使用生产构建和 `FASTCAS_UI_DIR`。

真实浏览器验证在仓库根目录执行，需要独立 PostgreSQL 测试库和 Chrome（可用 `CHROME_BIN` 指定）：

```sh
FASTCAS_TEST_DATABASE_URL='postgres://.../fastcas_test?sslmode=disable' \
FASTCAS_BROWSER_CONTRACT=1 \
go test ./internal/httpapi -run '^TestConsoleAgainstRealBrowser$' -count=1 -v
```

该契约使用隔离 schema、真实 HTTP 和构建产物，覆盖注册、登录、改密、恢复、MFA、分页、审计筛选、应用配置保存/失败重试/取消、焦点恢复、密钥轮换与清除、服务生命周期和设备确认。也验证禁用 JavaScript 时的原生登录、桌面深浅主题和主题持久化；延迟应用模块检查 console/login 的保存偏好、系统默认、无效值和存储拒绝首屏，以及首屏 controller 同一性和安全表单跨主题保持。截图和 `integration-results.json` 保存在 `.local/ui-verification/`，只含隔离测试数据。`TestAuditFiltersAndPagination` 验证服务端时间边界、组合筛选、分页、非法条件与权限；`TestConsoleAuthManifestValidation` 验证产物完整性与路径约束。

这轮完成控制台基础重构与审计检索，不代表全部商业化规划完成。真实域名部署、监控告警、备份恢复和跨项目容量/SLO 验收仍需按项目交付清单推进。

图标使用锁定版本的 `lucide-react@1.72.0`，由 `Icon.tsx` 统一 16px 操作图标、1.75px 描边和 `currentColor`，18px 标题图标及品牌盾牌沿用同一图形语言。图标随浅深主题、选中与禁用状态继承颜色；带文字的装饰图标使用 `aria-hidden`，保留控件原有可访问名称。
