# API 与三语言 SDK 规划

本文是拟定契约，不是已存在的 API 或可安装包。标准端点名称由实现配置与 discovery 最终给出；应用不得把端点地址散落硬编码。

## 标准协议与业务 API

| 类型 | 规划端点 | 使用者与要求 |
| --- | --- | --- |
| Discovery / keys | `/.well-known/openid-configuration`、`/oauth/jwks` | SDK 固定 issuer；JWKS 缓存与受限刷新 |
| Web 登录 | `/oauth/authorize`、`/oauth/token`、`/oauth/userinfo` | Code + PKCE + state + nonce；服务端兑换 code |
| 撤销/检查/注销 | `/oauth/revoke`、`/oauth/introspect`、`/oauth/end-session` | 按注册 client 授权，不允许任意服务检查别的 audience |
| 本机/CLI | `/oauth/device/code` + token endpoint | 后续本机集成阶段启用；public client 无长期 client secret |
| 绑定意图 | `POST /api/v1/link-intents` | 应用服务身份、固定自身实例、本地近期认证证明上下文；返回 opaque intent |
| 绑定确认 | `POST /api/v1/link-intents/{id}/prepare`、`.../activate` | prepare 需该事务对应的 CAS 用户证明和原应用身份；activate 需本地 pending 已提交 |
| 状态/撤销 | `GET /api/v1/account-links/{id}`、`POST .../{id}/revoke` | 应用仅访问自身 link；用户仅访问自己的关联；幂等与版本检查 |
| 个人安全中心 | `/api/v1/me`、`/api/v1/me/links`、`/api/v1/me/sessions` | 自身服务端会话 + CSRF；资料更新不得改变 subject |
| 管理 | `/api/v1/admin/applications`、`.../identities`、`.../service-accounts`、`.../audit-events` | 管理员角色、敏感操作近期 MFA、审计；不通过普通项目 SDK 开放管理权 |

项目侧的统一建议入口：`/auth/fastcas/login`、`/auth/fastcas/callback`、`/account/fastcas/link`、`/account/fastcas/unlink`、`/auth/fastcas/backchannel-logout`。允许适配项目现有 `/api/auth/*` 路径。

### 绑定证明的具体边界

`local_account_ref` 使用部署内稳定 opaque ID，不发送项目密码或 password hash。仅应用服务端可声明其本地用户已完成近期认证；浏览器不能提交一个任意 user_id 代替该证明。

绑定 intent 与 OIDC 请求由服务端绑定，授权页展示用途为 link，CAS 确认页把 subject 固定到此 intent。回调交换得到的证明必须对应同一 client、intent、nonce 与 subject；prepare 同时校验应用凭证及这份用户证明。禁止接受普通 client-credentials token 携带一个任意 subject 来代替用户确认。

本地端必须检查发起会话、浏览器绑定 cookie、本地 user_id、认证时间和 CSRF；CAS 端只能证明其受信应用的声明和 CAS 用户确认，不能独立验证项目内部密码。应用被攻破是独立信任边界，scope 与实例隔离限制影响范围。

API 错误结构规划为 `code / message / request_id / retryable`，不含 token；冲突 409，未认证 401，权限不足 403，前置版本失败 412，限速 429，不可用 503。绑定创建/激活/撤销支持 `Idempotency-Key`，绑定变更支持版本前置条件。标准 OAuth 端点继续使用标准错误格式，不混用管理 API 格式。

## 身份与授权表达

```json
{
  "issuer": "https://auth.example.test",
  "subject": "idn_example",
  "display_name": "示例成员",
  "email": "member@example.test",
  "email_verified": false,
  "auth_time": 1789990000
}
```

以上仅为 SDK 归一化身份示例，不是完整 ID Token。ID Token 的 aud 是 client；API Access Token 的 aud 是目标资源，两者不能互用。

应用 principal 必须同时保留 `local_user_id`、`auth_source`、可选 `fastcas_identity`、`link_id/version`。业务查询一律按本地 ID；不能把 CAS subject 直接填入历史外键。

建议授权分为：

- 基础 OIDC scope：`openid profile email`，按需申请。
- 绑定/身份 API：`account-links:write` 等，仅允许访问该应用自己的对象，不等于全局目录管理权限。
- 服务访问：如 `insight:publish`、`reading:publish`、`panel:summary:read`，由资源服务独立校验。
- 用户委托：令牌体现用户 subject、调用应用 actor、目标 audience 与缩减后的 scope；目标应用再通过 active link 找到本地用户并检查 ACL。

Client Credentials 只代表服务自身，不能凭调用参数 `person` 或 `user_id` 冒充用户。FastInsight 以服务身份写收件箱可用显式授予的投递权限，接收者是资源目标，不是被冒充的调用者。

跨项目代表用户调用采用受限 Token Exchange（[RFC 8693](https://www.rfc-editor.org/rfc/rfc8693.html)）：仅注册调用链、已有合法 subject token、用户授权、受限目标资源；目标 scope 是用户许可与客户端许可交集。没有绑定的本地用户继续原集成路径或单独连接项目，不能强制先注册 FastCAS。

## SDK 交付形态

| SDK（包名待发布前检查） | 运行时与底层 | 适用项目 |
| --- | --- | --- |
| `@fastrd/fastcas` | TS ESM；browser 与 server 子入口；服务端基于 openid-client/jose | Research Node、Write Bun；Read/Task React 页面可用轻量 browser helper |
| `github.com/FastR-D/FastCAS/sdk/go` | Go，OIDC 客户端/资源验证包装；net/http 核心、Gin adapter | FastTask 与其他 Go 服务 |
| `fastcas-sdk` / `fastcas` import | Python ≥3.10 规划基线；Authlib/HTTPX 候选；同步/异步 | Read FastAPI、News 原生 HTTP、Insight 脚本、Labs |

TS 底层选择依据：[openid-client](https://github.com/panva/openid-client) 明确支持 Node/Bun 等运行时；[jose](https://github.com/panva/jose) 提供 JOSE/JWT/JWKS 能力。Python 候选：[Authlib](https://github.com/authlib/authlib)。Go 验证客户端候选：[go-oidc](https://github.com/coreos/go-oidc)。具体组合由 P0 固定版本测试决定，客户端与服务端不要求使用同一家库。

Python SDK 不以 FastAPI 为强依赖：`[fastapi]` 是可选 extra。FastInsight 声明 ≥3.8，启用 SDK 的新部署需升至 ≥3.10；原脚本非 FastCAS 路径不因这一改造强制迁移。Go 基线及 FastTask 升级见架构文档。TS SDK 明确测试 Research Node 与 Write Bun，不能只在 Node 验证。

### 三种 SDK 的共同能力

| 能力 | 内容 |
| --- | --- |
| `beginLogin / finishLogin` | 创建及验证事务；PKCE、nonce、state、回调绑定；返回身份，不默认创建本地用户 |
| `beginLink / finishLink` | 用途与 login 分离，携带本地账号上下文，执行准备/激活及恢复 |
| `getLink / revokeLink` | 验证关系、版本、幂等撤销；不删除本地业务账号 |
| `verifyAccessToken` | issuer/audience/type/alg/时间/scope 检查，JWKS 更新及失败关闭 |
| `clientCredentials / exchangeToken` | 凭证轮换、scope/resource 限制、短期缓存；不把 secret 放入浏览器 |
| `handleLogout / handleEvent` | 签名、重放、版本、防重复、终止指定来源会话 |
| `deviceAuthorize` | 后续本机阶段使用，处理 pending、slow_down、denied、expired |
| `health / diagnostics` | 脱敏配置检查；输出缺失项，不输出凭证 |

浏览器 SDK 只提供跳转、自身项目会话状态和组件辅助；不能内置 client secret、签名密钥或调用管理员 API。服务端 SDK 不自动替应用安装全站鉴权 middleware；认证关闭时不能改变原 HTTP 行为。

### 接入伪代码（设计说明，尚不可执行）

```typescript
// FastWrite / Research 服务端：原登录接口照常存在
const identity = await cas.finishLogin(request, transactionStore);
const local = await links.findActive(identity.issuer, identity.subject);
if (!local) return showBindOrCreateChoice();
await accounts.requireActive(local.userId);
return sessions.create(local.userId, { source: "fastcas", linkId: local.linkId });
```

```go
// FastTask：SDK 返回身份，项目决定本地用户和权限
identity, err := cas.FinishLogin(ctx, request, transactions)
if err != nil { return err }
user, err := accounts.ResolveActiveLink(ctx, identity)
if err != nil { return showBindingChoice(err) }
return sessions.IssueForLocalUser(ctx, user.ID, "fastcas")
```

```python
# FastRead：沿用本地 workspace / membership，不用 CAS claim 替代它们
identity = await cas.finish_login(request, transactions)
user = await links.resolve_active(identity)
await users.require_active(user.id)
return await sessions.issue(user.id, source="fastcas")
```

### SDK 工程约束

事务/session store 通过接口注入，生产不默认使用进程内 Map；HTTP 超时和取消、代理/TLS、重试策略可配置。授权码兑换默认不盲目重试；绑定使用幂等键；JWKS 未知 kid 最多受限刷新，不能每请求无限拉取。JWT header 的 jku/x5u 不能决定下载地址。

绑定权限和角色不从客户端 cookie/参数直接信任。SDK 不自动按 email 合并，不自动注册管理员，不自动启用跨项目调用，不自动发送用户内容。

## 发布与兼容

建议单仓库包含 server、web、`sdk/typescript`、`sdk/go`、`sdk/python`、examples 与共享 contract fixtures。Go SDK 子模块发布 tag 形如 `sdk/go/v0.1.0`；三 SDK 与服务使用显式兼容矩阵，固定示例版本；包名和 GitHub 路径当前只是规划，不代表已占用成功。

OpenAPI 生成普通 DTO/client 后，手写认证层覆盖协议库；所有 SDK 运行相同成功/失败 fixtures。首发 v0.x 明确可能变更，形成稳定绑定/撤销契约后再发 v1。
