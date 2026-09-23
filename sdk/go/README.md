# FastCAS Go SDK

可运行的服务身份调用见 [三语言示例](../../examples/service-tokens/README.md)。

模块：`github.com/FastR-D/FastCAS/sdk/go`。本地开发通过主模块 replace 指向该目录，尚未发布远程 tag。

用 `New(Config, TransactionStore)` 创建客户端；应用提供原子 `Take` 的持久事务存储。`BeginLogin/FinishLogin` 完成 OIDC 验证；`BeginLink/PrepareLink/ActivateLink` 完成显式已有账号绑定。SDK 不创建用户、不改本地密码和业务权限。

回调必须绑定原浏览器随机 Cookie；绑定时还检查原本地 account/session。激活前提交本地 pending 关系，成功后写 active；跨库失败按相同 link ID 重试。应用应保留本地登录入口和独立会话来源。

显式注册使用 `BeginRegistration(ctx, newLocalAccountRef, options)`。应用自行检查注册政策并生成全新 ID；回调 purpose 为 `register`，prepare 后在本地事务内检查该 ID 不存在，创建普通用户和 pending 绑定，随后 activate。禁止拿已有账号 ID 绕过 `BeginLink` 的本地证明。

`HandleNotification(ctx, raw, apply)` 验证签名绑定撤销或身份状态通知后调用本地事务；旧 `HandleEvent` 仍仅接受绑定撤销。事务应按事件 ID 去重、按绑定 ID/版本更新，仅终止对应 FastCAS 来源会话；重复已处理事件返回成功。事务失败必须向 webhook 返回非 2xx，供服务重试。详见 [事件契约](../../docs/events.md)。

公开设备客户端可调用 `DeviceAuthorize(ctx, scopes)` 取得设备代码、验证地址与轮询间隔，并按间隔调用 `PollDevice(ctx, deviceCode)` 一次。`authorization_pending` 与 `slow_down` 以 `APIError.Code` 返回；成功时 SDK 同时校验 ID Token 与访问令牌的签名、issuer、audience、subject 和用户令牌边界。实际应用仍须在设备本地确认配对，不能以该令牌授权任意本机命令。

服务资源端先用 `VerifyAccessToken(ctx, raw, audience, scopes...)` 校验签名、issuer、受众、类型与 scope，再用 `IntrospectToken(ctx, raw)` 查询中心是否仍 active。introspection 使用保密资源客户端凭证；停用服务身份会使中心检查立即返回 inactive，而仅离线验签可能接受令牌直到五分钟到期。实际 Go/PostgreSQL 服务身份签发、停用和 introspection 契约已通过。

`Diagnose(ctx)` 可检查 discovery 连通性，只返回 issuer、client ID 和就绪状态，不包含密钥。紧急签名轮换后，可在可信运维流程暂停并排空认证请求，再对每个 Go SDK 实例调用 `ResetVerificationCache()`；它丢弃 discovery/JWKS 缓存，下次验签重新拉取公钥。已开始的验签以及项目已签发的本地会话不受该调用撤销，仍需项目侧会话处理。真实 Go/PostgreSQL/JWKS 轮换契约覆盖未知 kid 刷新、旧键保留、紧急清理后旧键拒绝与断网失败关闭。

当前已通过 Go HTTP 服务与 PostgreSQL 的真实登录、绑定、设备授权和事件投递集成测试。完整发布仍待补齐，不代表 SDK 全功能已完成。

Gin 项目可选择性导入 `github.com/FastR-D/FastCAS/sdk/go/ginadapter`。`Events(provider, apply)` 和 `Logout(provider, apply)` 处理签名通知的 Content-Type、64 KiB 上限、验签和响应状态；应用提供的 `apply` 必须在自己的事务里完成事件 ID 去重和 FastCAS 来源会话撤销。验签失败返回 401，本地提交失败返回 503 供 outbox 重试。`RequireAccessToken(provider, audience, scopes, introspect)` 只挂到指定资源路由；本地账号、ACL 和收件人限制仍由项目检查，`Claims(c)` 只返回已验过的令牌声明。FastTask 已使用通知适配器并通过真实提供方契约。
