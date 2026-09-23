# 七项目接入与数据迁移

## 共同接入规则

新增 FastCAS provider 与账号认证页，不替换本地账号主键。每个项目配置独立 client 和部署实例；至少增加外部身份映射、绑定事务、会话来源信息。所有新增表/字段采用可回滚的增量迁移。

已有本地用户默认处于“未认证”，不是待强制迁移用户。禁止批量按邮箱、姓名、飞书显示名关联；提供 dry-run 盘点可以发现候选，但实际绑定仍需用户双端证明。需要管理员辅助恢复的情况单独审计，不以导入文件代替用户证明。

| 项目 | SDK | 本地入口动作 | FastCAS 接入 | 难点 |
| --- | --- | --- | --- | --- |
| FastWrite | TS server + browser helper | 保留本地登录/注册 | 复用 OIDC port，增加绑定与撤销 | 现有自动新建外部账号、首次管理员、协作会话 |
| FastTask | Go + TS helper | 保留账号/管理员开户政策 | 新 provider + 本地 User 映射 | HS256/local 与 CAS RS256 严格区分；Device/Service 不混用 |
| FastRead | Python + TS helper | 恢复本地密码入口、配置邀请注册 | Web auth 接入 OIDC、绑定 | 当前 HTML 门禁和 403 登录路由；工作区不变 |
| FastResearch | TS server + browser helper | Key 登录保留；增稳定本地账号 | 可选 CAS 登录、认证管理 | Key/内容耦合与旧票据兼容 |
| FastNews | Python + 普通 JS | 新建轻量本地账号/会话 | 原生 HTTP adapter | 个人内容目前代理 Research，需拆清归属 |
| FastInsight | Python | 保留脚本/机器人凭证模式 | 可选服务账号；人类收件人映射 | 同名广播、受众授权、Python 基线 |
| FastLabs | Python + 普通 JS | 保留本机单用户模式 | 可选设备授权绑定本机操作人 | 本地执行安全边界；无公网回调与离线使用 |

## FastWrite：首个完整试点

改造位置：[identity-provider.ts](../../../FastWrite/apps/server/src/auth/identity-provider.ts)、[auth-service.ts](../../../FastWrite/apps/server/src/auth/auth-service.ts)、[app.ts](../../../FastWrite/apps/server/src/app.ts)、[database.ts](../../../FastWrite/apps/server/src/storage/database.ts)、[config.ts](../../../FastWrite/apps/server/src/config.ts)。

1. 新 SDK 实现既有 IdentityProvider port；保留已有通用 OIDC/CAS 配置，FastCAS 使用独立命名与 issuer。
2. 增加 `beginLink/finishLink` 路由，与 login callback 的 purpose 隔离。现有 local user 绑定到外部身份时复用 user ID，不能走 `loginExternal` 的自动新建分支。
3. `(issuer, subject)` 与本地账号冲突进入显式选择/恢复；外部首次登录创建普通用户，管理员仅通过 bootstrap/已有管理员授权产生。
4. 保留 localCredentials 与原 refresh；增加 `authSource/casSid/linkVersion`。上游 CAS token 存服务端；不把 CAS refresh token 转交前端。
5. FastCAS groups 默认不触发现有 `syncIdpGroups` 创建团队或授予权限；需要显式管理员配置映射后再开放。
6. 撤销 CAS 来源 session 时同步断开相关协作连接、失效 room grant；本地来源 session 不受单纯解绑影响。
7. JSON 默认模式与已启用 PostgreSQL 模式都覆盖迁移；没有启用的存储模式不强制切换。

验收：原用户注册、登录、项目编辑照常；绑定后用 CAS 登录进入同一个项目；解绑后仍能密码登录；不同账号同邮箱不能自动合并；首次 CAS 用户不是管理员。

## FastTask：验证 Go 集成与身份分类

改造位置：[internal/platform/auth/auth.go](../../../FastTask/internal/platform/auth/auth.go)、[internal/persistence/models.go](../../../FastTask/internal/persistence/models.go)、[internal/httpapi](../../../FastTask/internal/httpapi)、[web/src/api.ts](../../../FastTask/web/src/api.ts)。

1. Go 运行时升级作为独立前置变更通过现有测试；SDK net/http 核心由 Gin adapter 挂载。
2. 新增 external identities 与 link records，CAS 回调解析为现有 User；继续检查数据库 active 状态。
3. 保留 local HS256 会话验证；只在显式 CAS resource middleware 验证 CAS access token，禁止一个宽松验证函数接受所有 issuer/算法。
4. CAS 登录可复用本地 session 签发设施，但会话记录标记来源并支持按 cas_sid/link 撤销。
5. Device Token 仍只读；不因 CAS 登录发放设备权限。Panel/Import 服务接入后校验 audience、scope、调用应用和用户绑定，保留旧服务 JWT 路径在显式兼容配置下运行。
6. 本地用户创建、禁用、角色管理继续归 FastTask；不把 CAS 管理员映射为 admin，不要求新增公开注册。

验收：本地登录不请求 FastCAS；CAS disabled user 不能通过 CAS 登录但未禁用的本地账号可本地登录；FastTask disabled user 两条路径都拒绝；设备不能使用普通用户/服务令牌调用越权接口。

## FastRead：恢复独立入口，再接 FastCAS

改造位置：[app/web/api.py](../../../FastRead/backend/app/web/api.py)、[app/web/auth.py](../../../FastRead/backend/app/web/auth.py)、[app/web/store.py](../../../FastRead/backend/app/web/store.py)、[fastread-frontend](../../../FastRead/fastread-frontend)。

1. 让 `/api/auth/login-info` 返回实际可用 providers 与注册策略，不固定 `fastresearch-key`。
2. 修改 HTML gate 放行本地登录、注册/邀请、恢复、CAS 回调和静态必需资源；用户直访进入本项目登录页，不强制跳 Panel。
3. 将已有 password login 函数接回 Web 路由；邀请开户/恢复单独补齐 CSRF、限速和凭证校验，不能仅取消 403 就认为功能完成。
4. 旧 `research_key_id` 是历史关联，保留并转成独立 provider 映射；旧用户绑定 CAS 时不改 user/workspace/membership，也不修改原业务内容。
5. 新增 Python SDK 的 FastAPI 适配器；保留 `require_user` 和 workspace/owner 授权边界。
6. 老 Key-only 用户设置本地密码前，先完成有效旧 Key/旧 provider 近期证明，再绑定/开户；不能把伪造 SSO 邮箱当成可投递验证邮件地址。
7. worker 继续以本地任务/workspace 身份执行；不把用户短期 CAS token 持久化到长任务 payload。

验收：原论文/证据/专题仍属于同一用户和工作区；本地登录、CAS 登录、旧 Research SSO 三条路径按配置共存；关闭 CAS 不影响本地页面；错误 callback 不绕过 CSRF 或 gate。

## FastResearch：账号稳定化，保留门户职能

改造位置：[server/index.mjs](../../../FastResearch/server/index.mjs)、[src/App2.tsx](../../../FastResearch/src/App2.tsx)、[docs/key-login.md](../../../FastResearch/docs/key-login.md)、[docs/api.md](../../../FastResearch/docs/api.md)。

1. 建立本地 accounts 与 credentials；现存每个 Key 记录先映射一个独立本地账号，保留 legacy_key_id。多个同名 Key 不自动合并。
2. 把业务内容归属迁移到稳定本地账号，保留 Key hash 作为原登录方式；迁移不回收原始 Key，不输出密钥或哈希到审计报告。
3. 添加 CAS 登录按钮和账号认证设置；管理员 Key 管理界面只管理本地凭证，与 FastCAS 管理员界面分开。
4. 老 ticket issuer 与 consume 暂时保留，为尚未切换的 Read/News 服务；新 CAS 登录进入的本地账户也可经原允许的兼容路径使用门户，不要求所有项目同步切换。
5. 后续对接新的应用登录入口时由目标项目发起 OIDC，不在门户构造可复用 bearer token 传 URL。门户只传经过校验的业务 return path。
6. 发布 API 接受可选 CAS 服务令牌，但原 ingest key 可在独立配置继续运行；以本地 account ID 接收投递，姓名只显示。

数据迁移顺序：备份 access.json → 校验结构 → 生成 account/key/content 映射 dry-run → 事务/原子文件替换 → 数量与内容 hash 比对 → 保留旧映射回滚文件。正式启用前选择继续版本化 JSON 还是切 SQLite；本轮建议在本地账号模型稳定化时转 SQLite，避免新增绑定与并发会话仍依赖整文件改写，此变更需独立测试，不把数据库迁移放在登录 callback 中。

验收：Key 登录和旧内容完整；Key 轮换不改变账号；本地未绑定成员可继续打开已配置工具；门户离开 FastCAS 仍可用。

## FastNews：本地账号和个人资料边界

改造位置：[serve.py](../../../FastNews/serve.py)、[assets](../../../FastNews/assets)、[generate_homepage.py](../../../FastNews/generate_homepage.py)、[prompt/homepage.html.j2](../../../FastNews/prompt/homepage.html.j2)、[pyproject.toml](../../../FastNews/pyproject.toml)。

1. 新增小型 SQLite 本地账号、凭证、会话、外部绑定 store；登录/邀请注册/恢复页面独立于生成报告内容，不重复实现密码学，选固定维护库。
2. 拆出 AuthProvider 与 PersonalContentStore，支持 local、legacy Research、FastCAS optional 三类身份入口；未配置 CAS 不启动 discovery。
3. 现有 Research 的关注作者、印象、私信作为 legacy remote store 继续使用；新增本地账户有本地 store 默认值。原个人数据不因新增账号静默复制或覆盖。
4. 用户显式连接 Research 时，先证明原 Research 账号，再通过一次性导入快照拷贝个人内容或继续 remote 模式；记录来源、校验计数与 hash，避免双向写入两个主库。
5. 用户 CAS 登录后若未绑定 Research，不能声称已获得 Research 内容。绑定两个应用后，可按授权交换受限 token 调用连接器；禁止转发 CAS ID Token 充当原 Research session。
6. 新 Cookie 名使用 `fastnews_session`，停用新路径对 `fr_session` 的共享依赖；兼容路径显式转换，不直接透传所有认证头。
7. 静态公开周报/CI 生成继续无需 CAS；个性化服务与公开报告的访问政策分开。网页模板与生成器同时更新，避免下一次生成覆盖登录入口。

验收：CAS 关闭、Research 也未启动时，新本地用户可登录并使用本地报告/个人设置；LLM 等自身外部依赖另行判断；旧 Research 用户路径和数据仍可访问；连接器失败只影响连接功能。

## FastInsight：机器身份与收件人映射

改造位置：[publish_to_research.py](../../../FastInsight/scripts/publish_to_research.py)、[feishu_bridge.py](../../../FastInsight/scripts/feishu_bridge.py)、[scripts](../../../FastInsight/scripts)、[requirements.txt](../../../FastInsight/requirements.txt)。

1. Python SDK 可选启用：client credentials 获取 audience 为 Research API 的短期令牌，仅授予明确投递 scopes。
2. 原 `--ingest-key` 路径保留；启动明确选择一种认证策略，CAS 失败不自动降级成匿名或更高权限 shared key。
3. 新配置使用 receiver_ref（目标项目本地 ID，或经有效关联解析的 CAS subject），不按同名广播。服务允许的收件人范围由 Research 校验。
4. 飞书用户关联以 app/tenant/open_id 命名空间记录，需用户确认/上游证明；名册中的字符串本身不等于 FastCAS 认证。
5. 若名册没有 CAS 绑定，继续合法本地收件人路径；无需让全体机器人收件人注册 FastCAS。

验收：原 CLI 可运行；无 scope/错 audience/越范围收件人被拒绝；服务不能伪装任意用户；发送记录可追踪到 client 与投递授权。

## FastLabs：可选身份关联而非远程控制改造

改造位置：[server.py](../../../FastLabs/server.py)、[feishu_gateway.py](../../../FastLabs/feishu_gateway.py)、[web/app.js](../../../FastLabs/web/app.js)、[fastlab.env.example](../../../FastLabs/fastlab.env.example)。

1. 保留 localhost、单机、单操作人模式；设置页提供“关联 FastCAS”，不在每次启动增加登录墙。
2. 本机身份是安装生成的稳定 installation ID + 本机操作人记录；将其与 CAS 用户关联，不能用全局字符串 `local` 作为所有安装共享的绑定键。
3. 使用 Device Authorization，让用户在可信浏览器确认设备与绑定，避免中心回调私有机器；轮询遵守 interval/slow_down。[RFC 8628](https://www.rfc-editor.org/rfc/rfc8628.html)
4. Device flow 证明 CAS 用户，配对前还需本地短期能力码/本地确认，阻止其他网站跨站发起绑定；本机 HTTP 校验 Host/Origin、CSRF，不因 loopback 就视请求可信。
5. 若需调用跨项目 API，只允许该安装身份和用户授权的 scope；凭证优先 OS keyring，降级存储明确权限，不能分发一个全体安装共享 secret。
6. 解绑撤销跨项目 token，不停止已授权的本机任务、不删除 worktree。离线继续本地功能；同步功能明确不可用。飞书白名单和任务确认不因 CAS 认证而绕过。

验收：无网络/无 CAS 原功能可用；关联/解绑不启动任务；远程用户不能仅凭 CAS 身份执行本机命令。

## 灰度、兼容与回滚

先增量 schema 和 provider，再按实例开启入口；不一次替换七个项目。兼容策略显式保留 legacy/local/fastcas，token 验证按路径和类型路由，不尝试“用所有密钥挨个验到成功”。

迁移前备份并记录本地用户、关键业务对象、绑定和权限数量；以两个独立用户、同名/同邮箱用户、禁用用户、无本地凭证用户构建 fixtures。切换后校验同一 local ID 对应原数据。

回滚优先关闭新入口、停发新 CAS 会话，保留新增字段和原账号；避免回退数据库后继续接受新签发令牌。新增 CAS-only 本地账号在关闭登录入口前先配置恢复途径。错误撤销/解绑不能通过恢复旧备份自动重生；reconcile 尊重中心最新版本。

兼容路径退役要有调用统计和明确实例迁移状态；现阶段没有强制退役日期。本地登录始终不是待退役兼容路径。
