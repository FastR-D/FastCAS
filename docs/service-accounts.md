# 服务身份

服务身份用于项目之间的机器调用，不代表用户，也不要求任何项目把本地登录改为 FastCAS 登录。管理员在控制台“服务身份”页登记客户端 ID、允许的 scope 和业务资源受众，密钥只展示一次。创建、停用、重新启用和轮换均要求已登录管理员完成近期 MFA；写操作同时要求同源请求和 CSRF 凭证。

管理 API 为 `GET/POST /api/v1/admin/service-accounts`、`POST /api/v1/admin/service-accounts/{id}/status`（请求体 `{"active":false}` 或 `{"active":true}`）及 `POST /api/v1/admin/service-accounts/{id}/rotate-secret`。列表每页最多 100 条，用 `after` 传上一页末尾的 ID；响应不包含密钥。创建与重新启用返回一次性的 `client_secret`。常规轮换默认给旧密钥 10 分钟交接期；停用会立即清除旧密钥交接期，重新启用只能使用新密钥。

服务身份只能使用 `client_credentials`，必须预先登记非空 scope 与资源受众，不允许 `openid`、`offline_access`、登录回调、退出回调或绑定事件地址。应用权限接口不能把服务身份改成可交互登录客户端，也不能反向转换。客户端用 HTTP Basic 调用 `/oauth/token`，指定 `grant_type=client_credentials` 和已登记的 scope；不签发刷新令牌。接收方须同时检查签名、issuer、audience、scope 及允许的客户端 ID。例如 FastInsight 使用 `insight:publish` 和 `research-api` 向 FastResearch 投递，并仍受 Research 本地收件人白名单限制。

停用会撤销中心存储的该客户端访问令牌及令牌族。中心 introspection 随即返回无效；旧密钥也不能再签发令牌。仅离线验证 JWT、没有查询中心状态的接收方可能继续接受已签发令牌直到其五分钟有效期结束。需要更快的业务停用时，接收方还应查询中心状态或在本地禁用该客户端 ID。此限制与项目独立本地账号无关。

验证：`go test ./internal/core -run TestServiceAccountLifecycleAndLeastPrivilege -count=1`、`go test ./internal/httpapi -run TestAdminServiceAccountHTTPAndTokenLifecycle -count=1`；真实 Chrome 控制台契约通过服务身份创建、停用、重新启用及只显示一次的新密钥。
