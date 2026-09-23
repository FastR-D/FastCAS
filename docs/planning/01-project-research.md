# 项目现状与代码依据

## 调研方法与范围

阅读 7 个浅克隆项目的 README、依赖清单、认证实现、入口路由、持久化结构和跨项目调用代码。以下为静态代码调研，未运行各项目，也未访问生产数据。FastPPT 不在本轮范围。仓库内出现的未来规划不作为已实现功能。

| 项目 | 本地 HEAD |
| --- | --- |
| FastResearch | `69d98564d41ac519ca43549483700b0ad19519a5` |
| FastRead | `7c085d47d7d6803f8fefda0a297c2a563a32fb81` |
| FastNews | `a4c3bd153fdc45c52e35710c7e7e8f5319033e6d` |
| FastWrite | `161e9f8864ac719cfb73e685b090656d2b7603bf` |
| FastTask | `5f670f66aeb546121fe77a84edcf37813d1feda2` |
| FastInsight | `40b95310925c55f6e07bb169fb953f333ff81caa` |
| FastLabs | `69f3118418acbd3af6acd5da0a9d438de741b98e` |

## 产品与技术栈

| 项目 | 产品形态 | 实际技术栈/持久化 | 当前身份入口 | 接入结论 |
| --- | --- | --- | --- | --- |
| FastResearch | 科研工作流门户、研究印象、作者关注、信箱 | React 19、TS、Vite、Tailwind；Node 原生 HTTP；`data/access.json` | 管理员密码；成员个人 Key；自制 JWT 和一次性票据 | 门户仍独立运营账号；增加绑定与 FastCAS 登录；不把业务数据移入 FastCAS |
| FastRead | Web 论文阅读、证据、专题、后台任务；保留历史桌面代码 | React 19、TS、FastAPI、SQLite、文件存储、独立 worker | 当前 Web 强制 FastResearch 票据；本地密码函数存在，但登录路由禁用 | 先恢复本地入口，再增加可选 Python SDK；保留 workspace 权限 |
| FastNews | 静态报告与 Python 服务、CLI/GitHub Actions 生产内容 | Python ≥3.13、uv、原生 HTTP、HTML/JS、JSONL；部分 Node API 文件 | `serve.py` 消费 Research 票据，复用其会话并反代个人 API | 缺独立账号库；新增轻量账号/会话层，原 Research 模式继续兼容 |
| FastWrite | 多用户论文写作与协作、Agent、LaTeX | Bun ≥1.3、TS、React 19、Vite、Yjs；JSON Database，存在 PostgreSQL mirror/cutover 路径 | 本地注册/登录、刷新会话、OIDC 和 CAS provider；认证总开关 | 最适合绑定试点；复用 IdentityProvider 和业务 ACL，处理已存在账号而非另建账号 |
| FastTask | 科研目标、任务树、每日计划、设备摘要、Agent Job | Go 1.24.1 声明、Gin、Huma、fx、GORM、SQLite；React/Vite/mdui | 本地账号/JWT、刷新会话、独立 Device Token、服务 JWT | Go SDK 为新增 provider；User/Device/Service 三类身份继续隔离 |
| FastInsight | 飞书研究资讯分拣脚本/skill/长连接机器人 | Python，README ≥3.8；lark-oapi；名册 YAML | 飞书应用凭证；共享 ingest key；按成员姓名投递 | 服务账号认证优先，收件人可按显式绑定定位；无需强造网站注册页 |
| FastLabs | 本机单用户 Agent 调度台 | Python ≥3.10、原生 HTTP、SQLite、原生 JS、CLI/worktree、飞书白名单 | 回环地址本机模式与飞书 open_id allowlist | 可选操作人关联；不因接入而开放远程执行或强制联网 |

依赖清单的版本只是此快照声明，不能据此断言当前环境已安装或生产正在使用。

## 关键代码事实

### FastResearch：凭证、身份与业务内容耦合

[server/index.mjs](../../../FastResearch/server/index.mjs) 中：

- `defaultData`、`createMemberSession`、`requireMember` 以 Key 记录为成员身份，内容也存入该记录。
- `ensureKeyCollections` 维护关注作者、印象、信箱等业务字段。
- `/api/sso/ticket`、`/api/sso/launch` 与 `/api/sso/consume` 使用内存 `tickets`，票据 TTL 为 2 分钟。
- `/api/insight/publish`、`/api/workflow/publish` 使用服务共享密钥并按 `person` 匹配目标；空目标在部分逻辑中可命中多个成员。
- 成员 Cookie 与 News 默认同名 `fr_session`；Cookie 不以端口隔离，本机不同端口也可能互相覆盖。

接入必须先引入稳定本地 `account_id`，把 Key 变为登录凭证；删除/轮换 Key 不应删除账号和业务内容。同名匹配不能充当 FastCAS 绑定证明。依据：[README](../../../FastResearch/README.md)、[SSO 文档](../../../FastResearch/docs/sso.md)。

### FastRead：不能误把历史桌面认证接到当前 Web

[backend/main.py](../../../FastRead/backend/main.py) 实际调用 `app.web.api.create_web_app`。

[app/web/api.py](../../../FastRead/backend/app/web/api.py) 的 `sso_document_gate` 把无会话 HTML 请求跳到 Panel；`POST /api/auth/login` 当前直接返回 403。它不是已可用的密码登录入口。

[app/web/auth.py](../../../FastRead/backend/app/web/auth.py) 已有 `create_user`、`login`、scrypt、会话哈希和 CSRF；`login_from_research` 按 `research_key_id` 查用户，并建立工作区。绑定时应增加外部身份映射，不能改写 user/workspace ID。

[app/services/auth.py](../../../FastRead/backend/app/services/auth.py) 中 `local_desktop`、`SharedAuthProvider` 是另一条路径，不能据此认定当前 Web 已支持可插拔登录。

### FastNews：登录与个人内容代理是两件事

[serve.py](../../../FastNews/serve.py) 的 `consume_ticket` 接收 Research 返回的 session；`session_valid` 调 `/api/content/me`，并短暂缓存成功结果。`_proxy_api` 路径转发认证头/Cookie；领域导读、每日推送等还会读取 Research 业务信息。

因此“加独立登录”不会自动消除业务对 Research 的依赖。规划需要明确本地个人资料存储及可选连接器；不能拿 FastCAS Token 直接替换现有 Research JWT。依据：[README](../../../FastNews/README.md)、[pyproject.toml](../../../FastNews/pyproject.toml)。

### FastWrite：已有联邦身份入口，但绑定流程不足

[identity-provider.ts](../../../FastWrite/apps/server/src/auth/identity-provider.ts) 定义 `IdentityProvider`，实现 OIDC code/PKCE 和 CAS；OIDC 当前要求 HTTPS 且验 RS256。

[auth-service.ts](../../../FastWrite/apps/server/src/auth/auth-service.ts) 的 `loginExternal` 按 `(issuer, subject)` 查身份，没有匹配就新建用户，不等于“绑定当前已登录账号”。新用户首次成为管理员的行为也要在接入中改为显式 bootstrap，避免首个外部登录者获得平台管理权。

[app.ts](../../../FastWrite/apps/server/src/app.ts) 已有 `/api/auth/oidc/*`、`/api/auth/cas/*`、本地注册与登录、组同步；[config.ts](../../../FastWrite/apps/server/src/config.ts) 中 `FASTWRITE_SERVER_AUTH` 默认不启用。应分别验证本地模式与开启认证模式。

[database.ts](../../../FastWrite/apps/server/src/storage/database.ts) 保存用户、外部身份、会话、团队、项目成员和 ACL。FastCAS 登录后仍由这些本地规则决定资源访问。

### FastTask：不要把服务令牌当作用户登录

[auth.go](../../../FastTask/internal/platform/auth/auth.go) 使用 Argon2、HS256 JWT、数据库会话检查；service token 有 `represented_user_id`、`client_id`、scope 与独立 audience。

FastCAS 需要映射到现有本地 User，再签发/管理本地会话。Device Token 继续只读；服务身份必须有调用权限与合法用户委托，不能仅凭传入 user_id 获得用户权限。依据：[README](../../../FastTask/README.md)、[go.mod](../../../FastTask/go.mod)、[前端 API](../../../FastTask/web/src/api.ts)。前端当前持久化刷新令牌的行为需在接入时单独评估，FastCAS 上游刷新令牌只留服务端。

### FastInsight 与 FastLabs：非典型网站

[publish_to_research.py](../../../FastInsight/scripts/publish_to_research.py) 当前发送 `X-FastInsight-Key` 和 `person`。新模式采用服务身份和接收者映射；旧路径保留可配置兼容。

[FastLabs/server.py](../../../FastLabs/server.py) 默认监听 `127.0.0.1:8787`，与 Research 默认端口相同；[feishu_gateway.py](../../../FastLabs/feishu_gateway.py) 校验飞书白名单。接入不能绕过本机任务确认、CLI 权限或飞书白名单。

## 从现状得到的设计约束

1. 本地账号是业务数据锚点，FastCAS 绑定可增加、撤销；账号数据不随之移动。
2. 未绑定、FastCAS 不可用、用户拒绝认证均不应阻断已有本地功能。
3. FastRead/News 本地登录能力需要恢复或新建；不能描述成零改动接入。
4. Research/News 的身份兼容与内容迁移必须分开交付。
5. 不统一各项目的 workspace/team/role 表，也不集中存放模型、飞书或 GitHub 密钥。
6. 各部署实例独立注册应用，同一个人的两套 FastRead 部署不共享本地 ID 命名空间。
