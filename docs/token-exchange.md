# 受限 Token Exchange

FastCAS 的 `/oauth/token` 支持 RFC 8693 的受限访问令牌交换。调用方必须是保密客户端，登记 `urn:ietf:params:oauth:grant-type:token-exchange`、目标 `resources` 和可请求的 scope。管理员以近期 MFA 在控制台“调用策略”明确登记 `caller_client → target_client / resource / scope`；用户在 FastCAS“跨项目委托”页看到两个项目均已绑定后，逐项允许或撤销。关闭 FastCAS 或不使用跨项目调用的项目不需要此功能。

调用方提交自己持有的用户访问令牌，`subject_token_type` 和 `requested_token_type` 都是 `urn:ietf:params:oauth:token-type:access_token`，`resource` 与 `audience` 必须相同且只有一个目标。FastCAS 校验源令牌仍有效、属于调用客户端和活跃用户、非服务令牌及非再次委托令牌；请求 scope 还必须存在于源令牌，且符合应用登记、两个活跃绑定、管理员策略和用户同意。结果仅有五分钟访问令牌，没有刷新令牌；JWT 含用户 `sub`、目标资源 `aud`、受限 `scope`、调用应用 `act.sub` 和来源 `sid`。资源服务还必须验证 issuer、audience、scope、actor、项目本地绑定与 ACL，不能按同名或邮箱映射本地账号。

TS、Python、Go SDK 均提供 `exchangeToken` / `exchange_token` / `ExchangeToken` 包装，TS SDK 另提供资源端 `introspectToken`。Go/PostgreSQL 真实协议测试覆盖成功、源令牌缺 scope、错误 scope/资源、刷新令牌请求、CSRF、同意撤销、目标绑定撤销、源令牌撤销和 Go SDK。Node/Bun 的 TS SDK 与 Python SDK 也完成真实服务交换契约。中心 introspection 会在源令牌或目标绑定撤销后拒绝已交换令牌；纯离线 JWT 验签仍可能在原五分钟有效期内接受该令牌，因此敏感资源应使用 introspection 或更短本地缓存。FastNews → FastResearch 的只读连接器已通过真实三进程联调；生产部署与故障演练仍需验收。
