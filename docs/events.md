# 账号绑定与身份状态事件

FastCAS 在绑定撤销或身份状态切换事务中写入 outbox，后台 worker 向应用登记的 `events_uri` POST `application/jwt`。`account_link.revoked` 带完整绑定及单调绑定版本；`identity.status_changed` 带 `subject`、`status`（`active`/`disabled`）及该身份单调递增的 `version`。相同状态的重复管理操作不产生新事件。

JWT 使用服务 JWKS 对应的 RS256 密钥，header `typ=fastcas-event+jwt`；claims 包含 `iss`、应用 ID `aud`、稳定事件 ID `jti`、当次投递的 `iat`/五分钟 `exp`，以及 `event: {type, link}`。重试重新签名但保持事件 ID。接收方必须核对 issuer、audience、算法、类型、时效和 link.client_id，不能把普通登录 JWT 当成事件。

接收方在一个本地数据库事务中完成：按事件 ID 幂等检查、对绑定撤销比较绑定 ID 与版本、更新对应 FastCAS 绑定及其来源会话、记录事件已处理。身份停用事件仅终止相应身份的 FastCAS 来源会话；重新启用事件不会恢复旧会话，下一次登录仍须由中心重新验证。重复事件也返回 2xx。低版本或旧绑定事件不能撤销后来创建的新绑定。不要删除本地用户、修改业务数据归属或撤销纯本地登录会话。

三语言 SDK 的 `handleNotification` / `HandleNotification` / `handle_notification` 验签并分派两种事件；旧 `handleEvent` / `HandleEvent` / `handle_event` 保持仅接受绑定撤销。回调负责上述事务。回调失败会向上传递异常，HTTP handler 应返回非 2xx，使服务继续重试；不要先在另一个事务里写“已处理”，否则失败重试可能永久丢失业务更新。

服务端用 PostgreSQL `SKIP LOCKED` 领取事件，租约 30 秒，单次 HTTP 超时 10 秒，不跟随重定向。只有 2xx 表示确认；失败按指数退避，12 次失败后设置 `dead_at`，保留载荷供后续人工恢复。没有配置接收端点也算投递失败。投递是至少一次，不能依赖只发送一次。

## 全局退出与身份停用

`POST /api/v1/me/logout-all` 和管理员停用身份会在同一 PostgreSQL 事务中撤销中心浏览器会话、刷新令牌族与访问令牌，并为活跃绑定的客户端写入 `logout` outbox。身份停用另外写入 `identity.status_changed`，供项目同步状态；这两种通知独立重试。普通 `/api/v1/logout` 与单会话撤销只撤销对应中心会话及其派生令牌，通知中带该会话的 `sid`。客户端需登记独立的 `backchannel_logout_uri`。worker 向该地址 POST `application/x-www-form-urlencoded`，字段 `logout_token` 是标准 OIDC back-channel logout JWT；header `typ=logout+jwt`，claims 包含 `iss`、客户端 `aud`、`sub`、`jti`、`iat`、五分钟 `exp`、`events: {"http://schemas.openid.net/event/backchannel-logout": {}}`，全局退出不带 `sid`。`logout` 与应用事件不能共用验签类型或请求格式。

项目接收端必须验签并核对 issuer/audience/时间/事件类型、拒绝 nonce，再在一个本地事务中按 jti 去重和删除相应身份的 FastCAS 来源会话。`sid` 存在时只删该 sid；本地密码/Key 会话、账号、内容、权限保持原状。重复通知返回 2xx。投递使用与绑定事件相同的持久重试和死信机制。FastWrite、FastTask、FastRead、FastResearch、FastNews 已接收；FastResearch 和 FastNews 的真实 Go/PostgreSQL → 项目 SQLite 跨进程投递契约通过。其余项目的真实跨进程投递、部署 URL 登记及浏览器退出体验仍待验收。

## 投递时延与故障验收

`TestFastTaskIdentityStatusAgainstProvider` 在真实 FastCAS/PostgreSQL → FastTask Go/SQLite 链路中，让接收端第一次返回 HTTP 503，随后原样转发第二次签名事件。测试核对 outbox 两次尝试、首次 503、第二次项目提交成功、FastCAS 来源会话及刷新令牌失效、本地来源会话保留，并从数据库 `created_at` 到 `delivered_at` 测量重试后的到达时间。race 模式连续三次隔离演练为 2.06–2.18 秒，小于规划的 30 秒目标。FastWrite、FastRead、FastResearch、FastNews 的正常身份状态事件同样逐项测量了入队到项目确认时间（本地本轮约 0.02–0.10 秒）。这些不是生产网络与负载下的时延承诺。

运维可定时执行 `fastcas check-outbox`，根据最老未投递事件、死信数量和近期 P95 投递时延的阈值获得非零退出码，见[生产部署样例](production-compose.md)。成功投递满 90 天后分批清理，待投递与死信保持可见。

## 死信运维 API

- `GET /api/v1/admin/outbox`：默认只列死信，`state=all` 查看全部状态。最多 100 项；使用返回的 `next_cursor` 作为下一页 `before`。按事件 ID 降序分页，不代表创建时间顺序。
- `GET /api/v1/admin/outbox/{id}/attempts`：查看最多 100 条最近尝试的结果、HTTP 状态和耗时，不返回载荷、签名令牌或响应体。
- `POST /api/v1/admin/outbox/{id}/retry`：将指定死信重新排队，成功返回 `{"status":"queued"}`。不是死信、已投递、存在有效 worker 租约或不存在时返回冲突。调用前应修复接入方故障。

三个接口均要求管理员及近期 MFA；POST 同时要求同源 Origin 与当前会话的 X-CSRF-Token。列表仅返回事件 ID、应用 ID、次数、HTTP 状态及时间，不返回事件载荷、签名 token 或用户绑定信息。

重试保留原事件 ID 和载荷，因此接入方继续按 jti 幂等处理；重置本轮 attempts/退避/死信标记，同时保留前一轮逐次历史。重排与 `outbox.retry` 审计在同一数据库事务中提交，并发重复请求只有一次成功。失败审计回滚整个重排，不会静默丢失死信。管理员控制台已提供死信列表、逐次历史和重新排队。

测试：`go test -race ./internal/core -run TestDeadEventRetry -count=1 -v` 覆盖并发、租约、已投递、载荷保留及审计回滚；HTTP 测试 `TestOutboxAdminEndpointsRejectAnonymousAndMember` 覆盖匿名和普通用户拒绝。
