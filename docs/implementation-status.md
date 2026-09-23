# 实施与验收记录

目标：完整实现 planning 下的 FastCAS 服务、三语言 SDK，接入有登录机制的 FastR-D 项目，保持独立本地账号与可选认证。本文记录实际状态，不替代规划或缩小验收范围。

下文保留阶段性施工记录；旧段落中的“尚未完成”描述只代表写入时的状态，以顶部最新增量和各项目接入说明判断当前实现。

2026-09-23 七项目远端同步：逐个 fetch 并核对 `origin/main`，仅 FastTask 有 34 个新提交；本地 `main` 已快进到 `7b3d431`，其余六项目已对齐。FastTask 未提交的 FastCAS 接入已恢复并与上游 PWA、Agent harness 合并；原 CAS 迁移 6 改为 9，并增加旧迁移 6 数据库兼容升级测试。FastTask Go 全量测试、vet、前端 169 项测试、生产构建和隔离容器启动通过；真实 Chrome 契约确认 PWA 离线用户缓存仅为项目资料，访问令牌不落盘。完整 `make verify-contracts` 再次通过七项目回归及 28/28 真实提供方/浏览器契约。FastTask 合并前快照仍保留在 Git stash，原有未提交接入仍在工作树；未推送远端。

2026-09-23 本地发布产物增量：新增 `make release` 与 `scripts/build-release.py`，从已锁定源构建 Linux/amd64 服务二进制、Web 归档、npm TypeScript 包、Python wheel、Go SDK 源码快照及 OpenAPI，输出 manifest 和 SHA-256 清单；隔离目录实际安装/导入 Node 与 Python 包，解包编译 Go SDK。两次独立构建发现 wheel 元数据时间戳不稳定，固定 `SOURCE_DATE_EPOCH` 后所有产物与校验和逐字节相同。单仓库 CI 配置新增构建、验签与 artifact 上传步骤，但当前 FastCAS 无 Git 仓库/远程发布，Go 子模块 tag、npm/Python registry 发布、托管 CI 与七项目正式部署仍未完成；详见[发布说明](release.md)。

2026-09-23 密码恢复撤销范围补齐：管理员签发的 FastCAS 密码恢复成功时，除已有的中心会话和令牌撤销外，同一数据库事务还使待完成授权码、已批准设备授权失效，并向活跃关联项目排入标准退出通知。账号 ID、绑定关系及项目本地登录保持不变。真实 PostgreSQL 测试覆盖审计故障时密码、会话、一次性凭据和通知整体回滚，以及成功后的授权失效与退出通知。全项目并发门禁首次运行时，FastWrite 身份状态案例在项目进程退出与提供方确认投递之间出现时序失败；测试接收端现等待中心确认后退出，单项 race 连续五次通过，重跑完整 `make verify-contracts` 28/28 通过（含七项目本地回归）。

2026-09-23 持有人自行改密增量：安全中心新增“修改 FastCAS 密码”，使用当前密码及账号已启用时的动态验证码或恢复码。成功后事务性替换中心密码、撤销全部 FastCAS 浏览器会话/刷新及访问令牌/待完成授权，向已关联项目排入标准退出通知并记审计；项目本地密码、账号、内容与绑定关系保持独立。页面清除当前会话并提示用新密码登录。真实 PostgreSQL 核心测试覆盖错误密码和 MFA、旧会话失效、旧/新密码、项目退出通知及授权撤销；Chrome 契约覆盖安全中心改密与重新登录。OpenAPI 增至 46 个 JSON 操作；前端构建、Go 静态检查及完整 `make verify-contracts` 28/28（含七项目回归）通过。

2026-09-23 开发者接入页增量：管理员控制台新增“开发者接入”，读取真实 OIDC discovery 显示 issuer，说明每部署实例独立客户端、三语言 SDK 当前本地构建方式、可选登录与已有账号认证分流、持久事务/签名事件要求及常见错误。应用配置状态由管理员 API 读取并可翻页，明确只是登记状态而非端点健康检查；页面不读取或展示密钥，远程 SDK 包尚未发布。真实 Chrome/PostgreSQL 在 100 条以上应用配置下验证分页、issuer 和已轮换密钥不出现在向导；前端构建、管理员 race 浏览器契约及完整 `make verify-contracts` 28/28（含七项目本地回归）通过。

2026-09-23 管理员辅助 MFA 恢复增量：管理员经近期 MFA 才能签发一小时有效、一次性的 `mfa_reset` 凭证，仍须私下交付；持有人通过新公开页面输入该凭证与当前 FastCAS 密码，才能清除丢失设备的 TOTP 和旧恢复码。014 迁移扩展凭证种类。成功时单事务撤销中心会话、刷新/访问令牌、已批准授权码与设备授权，并为已关联项目排入标准退出通知；本地项目密码、账号、内容和绑定关系不变。真实 PostgreSQL 测试覆盖错误密码不耗凭证、重放拒绝、审计故障回滚及撤销范围；Chrome 从管理员签发走到持有人兑换、重新登录，并验证重新开通 MFA 前仍不能使用管理接口。OpenAPI 现为 45 个 JSON 操作；完整 `make verify-contracts` 通过 28/28 与七项目本地回归。单管理员同时具备密码恢复与 MFA 重置凭证签发权；生产环境须按组织信任模型约束管理员与私密交付渠道。

2026-09-23 MFA 恢复码维护增量：安全中心新增“更新恢复码”。仅同一活动浏览器会话在五分钟内完成密码和 MFA 验证后可轮换；PostgreSQL 事务删除旧码、插入 8 个新一次性码并写审计，失败时原码保持有效。真实数据库测试覆盖无 MFA 的另一会话、过期 MFA、旧码失效、新码单次使用及审计故障回滚；Chrome 从开通 MFA 到页面更新并确认新旧码不同。OpenAPI 现为 44 个 JSON 操作；完整 `make verify-contracts` 通过 28/28 及七项目本地回归。丢失 TOTP 且耗尽全部恢复码后的管理员辅助恢复仍未实现，不能把此功能当作完整账号恢复。

2026-09-23 管理列表完整性增量：安全中心管理员页面现在可继续加载身份、应用、服务身份和审计记录，不再只显示前 100 条；审计 API 新增按递减 ID 的 `before` 游标并纳入 OpenAPI，拒绝非法游标。身份状态、服务身份状态及应用权限/通知地址修改就地更新已加载行，避免操作后丢失后续页面。真实 PostgreSQL 填入每类至少 105 条记录，Chrome 在启用 MFA 后逐页看到旧身份、应用、服务和审计记录，非法审计游标被拒绝。前端构建、OpenAPI 43 路由检查、浏览器 race 契约、`go vet` 及完整 `make verify-contracts` 28/28（含七项目本地回归）通过。

2026-09-23 Outbox 积压与故障恢复烟测增量：`make verify-capacity` 现同时运行签名事件容量案例。真实 PostgreSQL 队列中 256 个事件由 8 个 worker 竞争领取，真实 HTTP 接收端逐一验签，全部投递且无重复/死信；单项投递阶段约 213 毫秒、到达 p95 约 205 毫秒。另一组 128 个事件先收到 503，保留待投递状态，按真实退避重试后全部成功，256 条尝试历史齐全；单项恢复阶段约 2.21 秒。整套容量入口与 `go vet ./...` 通过。生产网络和持续长时间积压仍待测量。

2026-09-23 登录/刷新突发容量烟测增量：新增独立 `make verify-capacity`，在真实 Go HTTP/PostgreSQL 下创建 64 个独立账号，使用独立环回源地址及浏览器 Cookie 会话，以 8 路并发完成授权码登录、同意及刷新；每一步必须成功。race 模式重复运行通过，最新一次登录 p95 2.41 秒、刷新 p95 433 毫秒，64 个会话的突发阶段约 15.25 秒。测试不修改现有每账号/来源 15 分钟 12 次的密码限流；首次用单账号尝试 64 次时如预期限流，随后改为真实独立账号场景。数据仅作本机基线，生产 TLS/代理、持续数百用户、事件积压和跨项目撤销压力尚未验收。

2026-09-23 三语言服务身份示例增量：新增 Go、Node/TypeScript SDK、Python 可运行示例，用环境变量申请限定范围的服务令牌、验签与检查受众和权限，并调用 introspection；输出仅含非敏感摘要。真实 FastCAS/PostgreSQL 契约逐一运行三个示例，确认正常路径成功且停用服务身份后三者均失败关闭。该契约已加入核心 CI 与全项目必过清单；完整 `make verify-contracts` 通过 28/28，同时通过七项目本地回归。另对 TypeScript SDK 执行 `npm pack --dry-run --json`，确认浏览器与服务端入口及类型声明进入产物；Python SDK 成功构建 wheel，Go SDK 两个包可由 `go list ./...` 列出。示例与本地验证过的运行时矩阵见 [examples](../examples/README.md)；三语言包正式发布与托管跨项目 CI 仍未完成。

2026-09-23 恢复围栏与 FastLabs 安装增量：`fence-restore` 现在在撤销快照凭证的同一事务内撤销设备安装记录；真实 PostgreSQL 测试确认安装管理凭据只能读到已撤销状态，重复围栏不会复活关联。若恢复的旧快照不含一台之后配对的安装，FastLabs 遇中心明确的 401/403/404 会清除旧映射和凭据；离线解除留下的待撤销记录收到此响应后可重新配对。网络不可达仍保留本地任务与待重试状态。FastLabs 配对单项 7 项与核心 race 恢复测试通过；跨主机快照与实际部署恢复仍待验收。

2026-09-23 安装清单完整性增量：安全中心安装清单改为每页 50 条的 `(created_at,id)` 稳定游标分页，响应含 `installations` 与 `next_cursor`，页面提供“加载更多安装”。PostgreSQL 索引迁移 013 支持按用户顺序扫描；真实数据库测试覆盖相同时间戳、翻页期间新增记录、无重复/遗漏及跨用户游标拒绝。真实 Chrome 在 FastLabs 配对后加载 56 条中心记录，确认第二页旧安装可见并从该页撤销旧安装，再撤销当前安装并等待本机状态收敛（最新 14.722 秒），任务列表不变。FastLabs 真实提供方契约、OpenAPI 43 路由校验、React 构建和 `go vet` 通过。生产规模与网络时延仍待验收。

2026-09-23 七项目本地回归门禁增量：将 FastWrite 13 项认证测试、FastTask 三个核心包 race 测试、FastRead 17 项账号/CAS 测试、FastResearch 8 项账号/投递测试、FastNews 16 项账号/内容测试、FastInsight 3 项服务授权测试和 FastLabs 51 项本机测试写入 `scripts/verify-project-regressions.sh`，并由 `make verify-contracts` 在 27 个真实提供方/浏览器契约前强制执行。七项目命令单独运行、新脚本整体运行及完整 `make verify-contracts` 27/27 均通过。FastCAS 尚无 Git 仓库、兄弟项目公开仓库尚不含本地接入改动，因此托管跨项目 CI 仍未实际交付，不能据本地门禁标记 P6 完成。

2026-09-23 设备安装撤销令牌补齐：中心撤销安装时同一 PostgreSQL 事务撤销登记所用的设备访问令牌，重复撤销也会修复历史不一致状态；012 迁移回填旧版已撤销安装尚活跃的令牌。真实数据库测试验证撤销后 `ActiveToken` 与设备 Bearer 校验立即拒绝，迁移前活跃令牌在升级后失效，FastLabs 本机主动解除流程仍通过真实提供方契约。聚焦 race 测试和 `go vet ./...` 通过。独立离线 JWT 验签者最多仍可接受到令牌五分钟到期，生产资源端的即时撤销需中心查询。

2026-09-23 FastLabs 安装安全中心浏览器验收增量：真实 Chrome 先在 FastLabs 本机设置页发起设备授权，再在 FastCAS 页批准和本机二次确认；随后打开同一 FastCAS 安全中心的“本机安装”清单，确认稳定安装 ID 与应用名称，从页面撤销并等待本机状态自动清除，最后核对任务列表不变。契约使用实际构建的 React 控制台、Go/PostgreSQL 提供方和 FastLabs Python/SQLite 服务；将有设置页访问时的中心状态检查间隔缩至 15 秒，测试限定 25 秒内收敛，最新隔离实测为 15.227 秒。聚焦 race 浏览器契约、FastLabs 配对单项测试与 `go vet ./...` 通过；生产网络时延仍待测量。

2026-09-23 FastLabs 中心安装管理增量：设备授权令牌现在标记来源 grant；本机二次确认后，Python SDK 用短期设备令牌登记唯一稳定安装 ID。FastCAS 记录安装、所属用户与一次性管理密钥摘要，安全中心“本机安装”支持清单和近期重新认证后的撤销；安装专用密钥只允许查询/撤销该行。FastLabs 将密钥写入 0600 文件，原设备访问令牌不落盘；本机解除先清理关联，断网时待撤销凭据会于后续联网重试。中心撤销后，本机下一次状态检查收敛，本地任务仍可用；旧的仅本机关联保持原状，用户可重配进入中心管理。真实提供方到 FastLabs 契约验证中心登记与解除；控制台构建、OpenAPI 43 路由及 FastLabs 单项测试通过，全项目 `make verify-contracts` 27/27 通过。生产多设备、离线长期保留与浏览器跨端验收仍待完成。

2026-09-23 失败重试到达时延增量：FastTask 身份停用跨项目契约在真实 Go/PostgreSQL → Go/SQLite 链路前加入 503 故障代理，首次签名通知失败、指数退避后第二次转发成功。契约逐项确认 outbox 两次尝试、第一次 HTTP 503、第二次项目事务完成、FastCAS 来源访问与刷新会话失效、原本地登录仍可用；从 outbox 创建到项目确认的隔离环境 race 模式连续三次实测 2.06–2.18 秒，小于规划的 30 秒目标。FastWrite、FastRead、FastResearch、FastNews 正常投递也记录了项目确认时延（本地本轮约 0.02–0.10 秒），五项目聚焦测试及全项目 `make verify-contracts` 27/27 通过；生产网络、并发容量及其他项目失败重试时延仍待测量。

2026-09-23 Outbox 监测与保留增量：新增只读 `fastcas check-outbox`，输出无事件 ID/正文的聚合 JSON，对超过 30 秒的未投递事件、非零死信和近一小时 P95 投递时延超过 30 秒返回非零退出码；阈值与窗口可配置，适合由生产监控定时执行并对失败告警。真实 PostgreSQL 命令测试覆盖空队列、超时、死信、慢投递和阈值调整；独立遥测测试区分到期与未来重试事件。维护任务每轮分批删除已成功投递超过 90 天的 outbox 及其历史尝试，待投递和死信保留供人工处置，PostgreSQL 回归通过。`go vet ./...` 与全项目 `make verify-contracts` 27/27 通过。生产告警接收平台、实际撤销时延与容量压测仍待验收。

2026-09-23 投递历史增量：新增 PostgreSQL `outbox_attempts` 迁移，租约持有者完成投递时与 outbox 状态原子记录尝试序号、结果、HTTP 状态及耗时；重试死信保留原事件 ID 与历史。管理员可通过近期 MFA 保护的 `/api/v1/admin/outbox/{id}/attempts` 和控制台查看最多 100 条近期记录，不返回事件正文、签名令牌或响应体。真实 PostgreSQL 并发、失败后手动重试、匿名/普通成员拒绝与 Chrome 管理员页面契约通过；OpenAPI 38 个接口核对、`go vet ./...` 与全项目 `make verify-contracts` 27/27 通过。故障告警、生产时延测量及长期历史保留策略仍待完成。

2026-09-23 FastNews 全站生成浏览器契约增量：验收进程在隔离临时目录实际运行 `render_homepage`，由真实 FastNews HTTP 服务交付首页和六个栏目页；Chrome 逐页确认页面可见、账号入口存在且相对路径正确，并从研究印象页点击进入账号页。随后沿原有本地密码、当前账号 FastCAS 认证、另一浏览器 FastCAS 登录、个人内容保持及解绑来源隔离流程验证。修复关注作者脚本离开页面时无变更也发起 PUT 的冗余保存，避免中断请求。FastNews 本地测试 16/16、聚焦 race 浏览器契约及全项目 `make verify-contracts` 27/27 通过；生产域名、真实数据迁移和视觉布局仍待验收。

2026-09-23 FastLabs 本机设备浏览器契约增量：真实 Chrome/FastLabs HTTP 与 SQLite/Python SDK/Go FastCAS/PostgreSQL 链路从设置页发起配对，在 FastCAS 页登录并批准后，仍需回本机明确确认才写入关联。验证稳定安装 ID、解除关联不改变任务列表及本机不被授权流程启动任务。全项目首次加入案例发现设置页定时刷新反复替换按钮，导致解除关联偶发不可点击；已改为设置数据未变化时保留原 DOM 节点，修复后 race 模式连续五次通过，全项目 `make verify-contracts` 实际通过 27/27。中心安装清单、跨项目授权、生产本机部署仍待完成。

2026-09-23 Go SDK 运维接口增量：新增脱敏 `Diagnose(ctx)` 与受信运维调用的 `ResetVerificationCache()`，与 TS/Python 缓存清理能力对齐。真实 FastCAS/PostgreSQL/JWKS 轮换契约验证 Go SDK 缓存旧公钥、未知 kid 自动刷新、紧急轮换后清理并拒绝旧令牌，以及断网时冷缓存失败关闭。清理前仍应暂停并排空认证请求；它不撤销项目本地会话。多进程部署协调仍待验收。

2026-09-23 FastWrite 项目浏览器契约增量：真实 Chrome/FastWrite Bun/JSON 存储/Go FastCAS/PostgreSQL 验证原本地密码登录、现有账号认证、独立浏览器 FastCAS 登录、原项目及用户 ID 保持、解绑仅撤销 CAS 来源会话。人为延迟授权回调后的 `/api/auth/refresh` 复现了前端过早挂载导致的未登录页面；现等待刷新完成后再挂载项目页。FastWrite 前端类型检查与生产构建通过，慢请求浏览器契约连续两次通过；全项目 `make verify-contracts` 实际通过 26/26。生产域名、真实数据迁移与协作跨进程撤销仍待验收。

2026-09-23 FastRead 项目浏览器契约增量：实际 FastRead Web 启动脚本此前在无会话时强制跳回 Research 门户，使本地邀请/密码与 FastCAS 登录页不可达；现无会话时加载本项目登录页，带 Research 票据的旧入口继续按原分支处理。真实 Chrome/FastRead FastAPI/SQLite/Go FastCAS/PostgreSQL 浏览器链路验证本地密码登录、原账号认证、独立浏览器 FastCAS 登录、原工作区及论文保持、解绑仅撤销 CAS 会话。前端类型检查、生产构建、71 项测试通过；浏览器契约在 race 模式连续三次通过。全项目 `make verify-contracts` 新增后实际通过 24/24；生产域名、真实数据迁移仍待验收。

2026-09-23 FastNews 项目浏览器契约增量：真实 Chrome/FastNews 账号页与 HTTP/SQLite/Go FastCAS/PostgreSQL 链路验证本地密码登录、当前账号认证、独立浏览器 FastCAS 登录、稳定本地账号及研究印象保持、解绑仅撤销 CAS 来源会话。该项目案例现纳入全项目必过契约，`make verify-contracts` 实际通过 25/25。控制台浏览器轮换密钥断言改为等待异步结果，控制台与 FastNews 两个案例连续运行三次通过；生产域名和全站生成仍需验收。

2026-09-23 FastTask 项目浏览器契约增量：真实 Chrome/Go FastCAS/PostgreSQL/Gin/SQLite 下，从 FastTask 页面以原管理员密码登录、打开账号认证、完成原生 FastCAS 授权回调，再在独立浏览器登录同一 FastTask 用户与角色。解绑后 FastCAS 来源会话刷新失败，本地会话仍可刷新。FastTask 前端生产构建和嵌套 Go race 浏览器测试连续两次通过；本地全项目 `make verify-contracts` 实际通过 23/23 必需契约。正式域名和真实任务数据的发布验收仍待完成。

2026-09-23 FastResearch 项目浏览器契约增量：真实 Chrome/Go FastCAS/PostgreSQL/Node FastResearch 下，用项目页面完成原 Key 登录、显式绑定入口和回调，再以独立浏览器上下文用 FastCAS 登录；账号 ID、Key ID 与原研究笔记保持不变。浏览器解除认证后 FastCAS 来源会话立即失效，原 Key 会话及研究笔记保持可用。测试发现并修复移动端登录后页头横向溢出。Chrome 还发现 FastCAS 登录页的 `form-action 'self'` 会阻断授权表单重定向到不同源的项目回调；现仅为已验证的当前授权请求添加其精确登记回调源，普通登录仍限本站。修复后原生浏览器确认和自动跳转契约在 race 模式连续三次通过；正式域名完整流程仍待验证。全项目 `make verify-contracts` 增加此项后实际通过 22/22。

2026-09-23 全项目验收入口增量：新增 `make verify-contracts`，在独立 PostgreSQL、真实 Go 服务、Node/Bun/Python/Go SDK、七项目进程与 Chrome 下运行，并逐项要求 21 个关键契约实际通过；缺失、跳过或失败不算通过。该入口本地实际通过 21/21。单仓库 CI 配置 PostgreSQL 16 并强制检查 10 个核心协议/SDK 契约，见[可重复验收](verification.md)。另有[单主机 HTTPS Compose 样例](production-compose.md)，已在隔离环境验证代理、issuer 与安全 Cookie；生产域名及 ACME、真实数据迁移、跨地域恢复、负载/时延指标及七项目托管 CI 仍未验收，P6 不勾选完成。

2026-09-23 生产代理限流修正：Caddy 覆写客户端地址并附上私有共享密钥，FastCAS 仅在密钥匹配且地址是单个合法 IP 时用于登录、设备验证码和一次性凭证兑换的按 IP 限流；伪造/重复/非法地址退回 TCP peer。配置解析、代理地址单元测试、Caddy 运行时伪造头覆写演练、服务端 race/vet 及全项目 `make verify-contracts` 21/21 通过。公网多级代理仍需明确 Caddy 的上游信任配置，当前部署样例仅假定 Caddy 直接面对用户。

2026-09-23 FastTask 接入契约补齐：其 Gin 实际挂载的 10 个 FastCAS 路由现全部进入 `/api/v1/openapi.json`，包含本地 bearer 可选/必需边界、密码证明、回调 Cookie 和签名通知的请求媒体类型。新增测试逐条对照 Gin 路由与 OpenAPI operation ID，更新有意变化的 golden 基线；FastTask HTTP、认证测试和 vet 通过。容器与浏览器发布验收仍待完成。

2026-09-23 FastTask 容器构建增量：修复 Dockerfile 引用不存在的入口脚本及两处前端产物路径错误，改用可复现的 Node 22/Go 1.26.8 构建阶段，并排除本地依赖与数据目录。通过 `--build-context fastcas-sdk=../FastCAS/sdk/go` 从锁文件完整构建镜像，隔离容器启动后 ready、SPA、10 条 FastCAS OpenAPI 路由均正常；关闭 FastCAS 的 `available=false` 与原本地管理员登录实测通过。正式域名、真实数据与浏览器认证流程仍待部署验收。

2026-09-23 FastWrite 旧账号补齐：无本地密码的 OIDC/CAS 账号可在五分钟内用原提供方新会话证明账号控制权并绑定/解除 FastCAS；刷新不延长证明窗口，已有本地密码不可绕过，只有 FastCAS 一种方式的账号仍不可直接解绑。FastWrite 专项 9 测、类型检查与全量构建通过；真实 FastCAS/PostgreSQL → FastWrite 契约覆盖旧 OIDC 登录、同邮箱不合并、旧项目权限保持、FastCAS 登录回原账号及原提供方会话解除绑定。扩大前端组件测试时发现 `no-raw-controls.test.ts` 在两个未改动组件的现存四处原生控件上失败，与本次认证改动无关；浏览器与正式发布验收仍待完成。

2026-09-23 FastWrite FastCAS 新账号恢复增量：近期五分钟内的 FastCAS 来源会话可为无密码账号原子增加本地密码，事务内复核活动绑定与版本；优先采用已验证且未占用的邮箱，否则生成随机本地登录 ID。重复设置、过期会话和已撤销绑定被拒绝。真实 FastCAS/PostgreSQL → FastWrite 联调完成开户、最后登录方式保护、跨站拒绝、添加本地密码、解绑后用本地 ID 登录同一用户及同邮箱旧账号隔离；浏览器与生产数据迁移仍待验收。

2026-09-23 全量浏览器契约稳定性修正：控制台测试在 Alice 退出后等待 React 显示未登录页面，再进入管理员登录，避免退出响应清理 Cookie 与新登录页设置表单 CSRF Cookie 交错。修正后连续两次运行 `make verify-contracts`，每次 21/21 必需真实提供方/浏览器契约通过。

2026-09-23 框架适配增量：Go SDK 增加可选 `ginadapter`，提供签名事件、back-channel logout 与仅挂指定资源路由的 access-token 中间件；Python SDK 增加可选 `fastcas.fastapi` 签名事件/退出传输适配，支持同步与异步 SDK。适配层控制媒体类型、64 KiB 上限、签名验证及本地提交失败的 503 重试响应；项目本地账号映射、事务去重和 ACL 仍由项目负责。FastTask Gin 与 FastRead FastAPI 现有通知路由已接入，二者本地测试及 FastCAS/PostgreSQL → 项目真实进程的登录、退出和身份状态事件契约通过。资源中间件在具体跨项目业务路由的生产运用、正式包发布仍待验收。

2026-09-23 SDK 撤销查询增量：Go 和 Python SDK 新增资源端 `IntrospectToken` / `introspect_token`，与已有 TypeScript 实现对齐。三语言均须先离线验证 issuer、签名、audience、token type 和 scope，再以保密资源客户端查询中心 active 状态；不能把 introspection 当作权限校验替代。Go 真实 Go/PostgreSQL 契约验证服务身份停用前后 active 变化；Python 真实提供方契约验证撤销后 inactive；TS Node/Bun 的真实服务 introspection 回归通过。独立项目本地登录不依赖这一中心查询。

2026-09-23 API 契约增量：新增 [OpenAPI 3.1 JSON 契约](../api/openapi.json)及生成/检查脚本，覆盖当前全部 37 个 `/api/v1/` 操作，含公开邀请/恢复、个人安全中心、绑定、应用/服务身份、身份管理、交换策略、审计和死信管理。检查器与 Go 实际路由表比对，校验操作 ID、schema 引用、路径参数及写接口 Origin/CSRF 声明；OAuth/OIDC 协议端点继续通过 discovery 获取。跨语言基于契约的 DTO/client 生成及正式发布仍待补齐，不能因此勾选 P6。

2026-09-23 服务身份管理增量：增加独立的服务身份清单、登记、停用/重新启用和密钥轮换 API 与 React 页面。只允许预登记的 `client_credentials`、非空 scope/资源受众，禁止交互登录、回调及 `openid`/`offline_access`；应用权限接口不能把服务身份转换为交互客户端。停用在同一事务中清除旧密钥及中心令牌，重新启用仅返回一次性新密钥。真实 PostgreSQL 测试覆盖管理边界、OAuth 实际签发与停用拒绝；真实 Chrome/Go/PostgreSQL 控制台契约覆盖登记、停用、重新启用和密钥不落 localStorage。仅离线验签的资源服务器可能在令牌剩余五分钟有效期内接受旧令牌，详见[服务身份](service-accounts.md)。多实例凭证分发与生产客户端部署仍待验收。

2026-09-23 身份状态增量：中心 `identities.status_version` 单调递增，停用/重新启用原子排入签名 `identity.status_changed` 事件；停用同时保留标准 back-channel logout。三语言 SDK 新增双事件验签入口，原绑定事件接口兼容；FastWrite、FastTask、FastRead、FastResearch、FastNews 事件接收端仅终止对应 FastCAS 来源会话，独立本地登录不受影响。提供方实际签名投递、Go SDK 真实服务验签、TS/Python SDK 字段拒绝及 FastWrite/FastTask 本地事务专项测试通过。五个账号项目均已通过 FastCAS/PostgreSQL → 项目进程/SQLite 或本地存储的真实状态事件投递契约：测试只登记 `events_uri`，不登记 back-channel logout，因而明确验证状态事件单独使 CAS 来源会话失效、本地登录与业务内容仍可用。原有真实登录/退出契约也再次通过；浏览器与生产投递仍待验收。

2026-09-23 本地部署/恢复增量：增加非 root 多阶段容器镜像、PostgreSQL Compose、数据库/密钥一致性备份及向新卷恢复的操作手册。两个隔离 Compose 项目完成实际备份、恢复、`fence-restore` 与紧急签名轮换；恢复后 ready、控制台、JWKS、管理员记录和审计通过检查。数据库专项测试覆盖恢复快照的会话、令牌、绑定与重复通知失效；独立项目本地账号不受影响。生产反向代理、异地恢复、监控告警、真实客户端缓存和会话协调仍待验收，详见[容器与恢复演练](compose-recovery.md)。

2026-09-23 FastNews 内容边界增量：本地账号收件箱在首次 GET 时可依据本项目个人资料生成每日论文，跨实例 SQLite 事务避免重复写入。用户可在账号页以 Research Key 显式证明旧账号，并一次性导入关注作者、研究印象和收件箱；默认拒绝覆盖，明确选择替换才写入。导入事务记录稳定 Research 账号 ID、数量和摘要，不保存 Key/临时 Research 会话。另有可选 CAS 委托读取：News 按会话加密保存刷新令牌，使用受限 token exchange；Research 验签、introspection、actor、scope 与本地绑定后仅返回只读个人资料。真实 FastCAS/PostgreSQL → News/Python/SQLite → Research/Node/SQLite 三进程契约覆盖授权、刷新、同步、撤销与本地来源隔离；生产部署和浏览器验收仍未完成，见 [FastNews 接入说明](../../../FastNews/docs/LOCAL-ACCOUNTS.md)。

2026-09-23 协议与接入增量：FastWrite、FastTask、FastRead、FastResearch、FastNews 五个账号项目均通过真实 Go/PostgreSQL 提供方到项目 HTTP 接收端的签名全局退出通知契约；FastTask 还验证刷新令牌失效。受限 RFC 8693 token exchange 增加注册调用链、显式用户同意、两个活跃绑定、单一资源/audience、源令牌 scope 交集与五分钟无刷新令牌；TS/Python/Go SDK 包装及 React 用户/管理员入口已加入。真实 PostgreSQL/HTTP 契约验证正反例、CSRF、同意、目标绑定与源令牌撤销；TS SDK 在 Node/Bun、Python SDK 也完成真实服务交换契约。News → Research 只读连接器已通过三进程联调。详见 [受限 Token Exchange](token-exchange.md)。

2026-09-23 退出增量：中心 `logout-all` 与身份停用在数据库事务中撤销中心会话/令牌并排入标准 OIDC back-channel logout；普通退出与单会话撤销带 sid 精确传播，Go/PostgreSQL 测试验证只撤销对应会话、保留其他会话及重复操作幂等。React 会话页增加全局操作入口，管理员可在登记或更新应用时配置 `events_uri` 与 `backchannel_logout_uri`；更新在近期 MFA、同源 CSRF 边界后执行，真实数据库测试验证无效 URL 拒绝及审计失败回滚。TS、Go、Python SDK 均可验签退出通知，五个账号型项目接收端按身份及可选 sid 原子去重，只撤销 FastCAS 来源会话。五个项目均已通过真实 Go/PostgreSQL 提供方到项目 HTTP 接收端的投递契约。客户端生产 URL 登记和全局浏览器验收尚待完成。

## 必须交付

- [x] P0 固定协议库/运行时；真实数据库一次性授权码、刷新轮换与绑定事务验证；至少两个运行时 RP 互通。
- [ ] P1 Go/PostgreSQL 服务；登录、邀请注册、恢复、MFA、安全中心；应用/服务身份管理、绑定、撤销、审计。
- [ ] P1 OIDC code/PKCE、discovery/JWKS、client credentials、refresh、introspection、revocation、logout；签名轮换、outbox。
- [ ] P2 TS（Node/Bun/browser）、Go（net/http/Gin）、Python（sync/async/FastAPI）SDK、契约 fixtures、示例与兼容矩阵。
- [ ] P3 FastWrite 可选认证/登录、绑定旧账号、解绑、会话来源与业务 ACL 回归。
- [ ] P3 FastTask Go 升级与可选接入；保留本地/设备/服务边界。
- [ ] P3 FastRead 恢复本地登录、邀请/恢复；CAS 与 legacy Research 共存、工作区不变。
- [ ] P4 Research 稳定本地账号、Key/业务内容分离、绑定与可选登录、旧 SSO 兼容。
- [ ] P4 News 本地账号与个人内容存储、可选绑定/登录、Research 连接器与生成器更新。
- [ ] P5 Device flow、受限 token exchange、服务授权与本机 SDK 示例；非账户型工具不强制新增登录墙。
- [ ] P6 管理/绑定 OpenAPI、React UI、可复现部署、迁移/备份/恢复/回滚、CI 与所有规划验收项。

## 当前证据

2026-09-23：新服务实际使用 Go 1.26.8、zitadel/oidc v3.51.3、独立 PostgreSQL 16 测试实例。FastWrite、FastTask、FastRead、FastResearch、FastNews 已实现初始可选接入并通过各自真实提供方契约；FastInsight 已实现受限服务身份投递；FastLabs 已实现本机可选身份关联。这不代表全部产品功能完成。SDK 已在 Node 22.22.2、Bun 1.4.0、Python 3.10.12 和 Go 中与真实测试服务互通。

已实现并验证：一次性授权码、PKCE、刷新轮换及重放撤销；已有账号绑定意图、prepare/activate、版本与唯一性约束、撤销及过期预约回收；邀请注册、保持用户 ID 的密码恢复、TOTP/一次性恢复码、近期 MFA 管理限制及最后管理员保护。三个 SDK 的核心登录和绑定链路均有真实服务契约测试。

新增 outbox 签名事件投递：数据库租约与 fencing、失败退避、12 次失败死信、禁止重定向泄露签名载荷。TypeScript、Go、Python SDK 均可验签并调用项目数据库事务处理绑定撤销事件；Python 异步接口等待异步事务提交。事件 ID 去重和绑定版本比较由接入方在同一事务内实现。并发投递、签名受众、事件类型、重定向拒绝、事务失败传播已覆盖测试，Go SDK 验证了真实服务投递。

仍未完成：全套应用与服务身份管理、死信管理界面、身份状态事件、其余跨项目连接场景、框架适配器、部署与迁移运维验收。已有 React 控制台、设备授权、受限 token exchange、News → Research 只读连接器、全局退出与 back-channel 及五个账号型项目初始接入；各自未完成的产品/生产部署/浏览器验收仍列于下文，不可将初始联调视为全量完成。

验证命令：`FASTCAS_SDK_CONTRACT=1 go test -race ./...`、`go vet ./...`、TS `npm test`、Python store unittest。数据库测试在缺少测试 DSN 时会跳过；完整验收必须配置独立数据库并检查实际运行输出。当前本地测试配置已提供该独立实例。

FastWrite 初始试点：已增加独立配置、SDK 持久事务、已有账号认证与撤销、FastCAS 登录按钮和账号认证弹窗、会话来源及刷新保留、版本化事件处理和当前进程协作连接重新授权。外部首次登录不再自动成为管理员。真实服务契约验证了本地注册 → 创建项目 → 绑定 → FastCAS 登录原用户 → 解绑 → 本地重新登录，并断言角色、项目权限和协作令牌边界。完整前端构建及类型检查通过。

FastWrite 新用户开户已增加独立注册策略（默认关闭）、`register` 事务和普通用户创建，三语言 SDK 均提供对应接口。真实联调覆盖同邮箱不合并、不继承旧项目权限、拒绝移除唯一登录方式，以及激活成功但响应丢失后的下次登录恢复。注册不会复用任意既有本地 ID。

试点剩余项参见 [FastWrite 接入说明](../../../FastWrite/docs/FASTCAS.md)：旧无密码用户证明、补设本地密码、全局退出/身份状态事件、跨进程撤销、真实 PostgreSQL 应用存储及浏览器验收仍未完成，P3 不勾选完成。新增真实联调命令：`FASTCAS_PROJECT_CONTRACT=1 go test ./internal/httpapi -run TestFastWrite -count=1 -v`。

FastTask 初始接入：升级 Go 1.26 后原后端测试通过；迁移 6 增加持久事务、唯一绑定、事件和会话来源字段。Go SDK 与 Gin 路由已接通已有账号认证、登录、解绑、签名事件、刷新会话族校验和中断激活恢复，保留管理员开户政策。真实 FastCAS/PostgreSQL → FastTask/SQLite 联调通过。前端增加入口但 npm 依赖下载超时，离线安装缺包，前端构建尚未验证；完整 OpenAPI、全局退出/身份状态、正式适配器及容器验收待完成。参见 [FastTask 接入记录](../../../FastTask/docs/fastcas.md)。

FastRead 初始接入：恢复本地登录、管理员一次性邀请/恢复及预登录 CSRF；旧 Research 账号可在新鲜票据登录后补充本地凭证，保留用户、工作区与后续 Research 入口。Python SDK 可选加载，接入持久事务、准备/激活、认证登录、解绑、签名事件原子去重以及五分钟绑定复核；关闭时本地会话不查询中心。16 项账号/服务/路由测试及真实 FastCAS/PostgreSQL → Python SDK → FastRead 认证路由契约通过。新增 TSX 通过 Bun 语法构建，但完整前端 typecheck/build/浏览器、完整应用依赖与路由回归、全局退出/身份状态事件仍未验证或未完成。参见 [FastRead 接入记录](../../../FastRead/docs/FASTCAS.md)。

FastResearch 迁移准备：新增 SQLite 离线迁移工具和只读报告，每个旧 Key ID 独立映射 account，保留全部内容/未知字段/旧 Key 标识及源文件备份，3 项测试通过。当前服务仍使用原 JSON，尚未交付 SQLite 运行时、账号/凭证轮换或 CAS provider，不计为完成接入。

FastResearch 运行时进展：SQLite 账号/凭证分离已接入原 HTTP 服务，Key 撤销保留内容、轮换保留 accountId；会话和 SSO 票据按摘要持久化，退出撤销、跨重启单次消费和凭证版本校验已实现。迁移/并发写入/真实 HTTP 生命周期 5 项测试通过；含当前数据回滚导出和签名密钥更新验证。Node 最低升级至 22.13，Docker 改为 Node 22 但未构建。FastCAS 登录/绑定与前端尚待完成，不能计为该项目已接入 CAS。

FastResearch CAS 存储层：新增 TypeScript SDK 事务适配、account ID 绑定唯一约束和撤销事件原子提交；故障回滚、幂等、来源隔离及旧事件保护测试通过。与现有运行时共 6 项测试通过；尚未连接 FastCAS 登录/绑定 HTTP 路由和界面。

FastResearch CAS 服务层：已有账号 Key 证明、SDK 绑定/登录/对账/撤销和五分钟有效性复核已实现，关闭时延迟导入且不访问中心。累计 7 项本地迁移/运行时/存储/服务测试通过。HTTP/前端和真实提供方契约尚未连接或验证，仍不计为完成接入。

FastResearch HTTP/前端进展：FastCAS 路由已接入原服务器；Key 与 CAS 来源分别校验，登录回调恢复、账号认证/重试/解绑界面已加入。SDK 本地依赖和 Docker 命名上下文已配置；离线完整 npm 安装缺包，只完成本地 SDK 导入与 TSX 语法构建，不能视为完整前端/容器验证。7 项后端本地测试通过；真实提供方联调仍待执行。参见 [FastResearch 接入说明](../../../FastResearch/docs/FASTCAS.md)。

FastResearch 真实协议验收进展：Go/PostgreSQL 提供方 → TS SDK → 真实 Node HTTP 子进程契约已通过，覆盖 Key 证明、同名不合并、绑定/登录/原内容、重放、原 Key SSO 和解绑隔离。联调发现并修复 CAS 来源经旧 SSO 被转成 Key 来源的问题；CAS 门户启动改为打开目标项目自身登录，Key 旧 SSO 保留。完整前端、容器、全局退出/身份状态和服务令牌投递仍待交付。

FastNews 独立账号基础层：新增 SQLite 本地用户、摘要会话、一次性邀请/恢复、个人内容隔离、管理员 CLI，采用 Werkzeug 3.1.8 scrypt。网络恢复后 uv.lock 更新并完成 Python 3.13.9 全部锁定依赖安装，3 项账号事务测试通过。尚未接入 serve.py/页面，legacy Research 和 CAS provider/连接器仍待交付。

FastNews HTTP 进展：本地认证接口已接入 serve.py，独立 Cookie、CSRF、请求体限制和退出处理经过真实 ThreadingHTTPServer 验证，累计 4 项测试通过。修复未消费拒绝请求体影响后续请求的问题，并阻止本地身份静默转入 Research 代理。页面、个人内容 API 与 CAS 尚待完成。

FastNews 本地页面/内容进展：独立登录邀请恢复页与个人内容 API 已接通，模板/生成器同步账号入口，公开报告不依赖外部登录；可配置本地登录门禁。真实 HTTP 组合测试验证本地资料、CSRF、公开/受保护页面与消息写入边界。浏览器视觉、自动内容投递、显式 Research 连接器及 CAS 仍待完成。

FastNews CAS 服务层：可选 Python SDK 依赖组已锁定并安装，持久绑定、密码证明、登录/对账/解绑与有效性复核已实现，累计 6 项本地测试通过。HTTP 路由、账号页面和真实提供方契约尚待连接，不计为完整 CAS 接入。

FastNews HTTP/页面：FastCAS 可选路由与账号入口已连接，受保护内容检查 CAS 会话有效性，本地登录独立。7 项测试通过，含开启配置下的 Origin/CSRF/密码证明和事件格式 HTTP 边界；真实提供方、浏览器及 Research 连接器仍待完成。

FastNews 真实协议验收：真实 Go/PostgreSQL 提供方 → Python SDK → FastNews HTTP 服务器契约通过，覆盖同邮箱不合并、原密码证明、绑定、同一账号/内容登录、回调重放拒绝与解绑后本地会话保留。8 项本地测试通过。修复访问日志泄露 code/state，实际联调日志已验证脱敏。浏览器、全站生成、Research 显式连接、本地消息投递及全局事件仍待完成。接入说明已整理为当前状态：[FastNews 本地账号与可选 FastCAS](../../../FastNews/docs/LOCAL-ACCOUNTS.md)。

FastInsight 服务身份投递：Python SDK 使用 `client_credentials` 获取 `insight:publish` 短期令牌；FastResearch 验签、校验 `research-api` audience、服务客户端与本地收件人白名单，按稳定 account ID 精确投递。CLI、自动分拣与飞书桥接共用显式凭证选择；启用 CAS 时名册缺 `receiver_ref` 不按姓名降级。旧 ingest key 路径保留。3 项自动分拣测试、8 项 Research 测试及真实 Go/PostgreSQL → Python SDK → Node/SQLite 投递契约通过。飞书用户的可信身份关联仍需另行实现；名册 ID 不构成上游证明。

设备授权与 FastLabs 初始接入：PostgreSQL 持久设备授权状态，公开客户端 grant、浏览器同源确认、轮询 `slow_down` 与原子单次消费已实现。Python 和 Go SDK 增加设备授权和单次轮询，TypeScript SDK 已有对应包装。FastLabs 设置页提供可选关联；稳定安装 ID 与已验证 subject 本机持久化，设备令牌不落任务数据库，二次本机确认后才关联。全部本机网页写接口新增环回 Host、同源 Origin 和 CSRF 校验。真实 Go/PostgreSQL → Python SDK → FastLabs 配对联调、Go SDK 真实设备契约、FastCAS 设备 HTTP 测试及 FastLabs 48 项回归通过。中心安装清单、跨项目授权、前端真实浏览器验收仍未完成，P5 不勾选。见 [设备授权与 FastLabs](device-flow.md)。

前端完整构建补验（2026-09-22）：网络恢复后 FastTask、FastResearch 的 npm ci 和 TypeScript/Vite 生产构建通过；FastRead 使用锁定的 pnpm 9.15.0 安装并完成 typecheck/build。FastTask 前端 22 项测试（新增 3 项 CAS 回调会话边界）、FastRead 前端 71 项测试、FastResearch 后端 7 项测试通过。此前记录的依赖下载阻碍已解除；浏览器、容器和完整业务接入验收仍未完成。

签名轮换初步交付：新增离线 `rotate-keys --retain 24h` / `--emergency` 命令与原子 keyring.json；保持加密密钥、保留有期限旧公钥、连续轮换及到期过滤，损坏 manifest 不回退旧键。专项测试通过。在线控制台、跨 SDK 未知 kid 刷新与紧急缓存失效尚未验收，完整密钥轮换规划仍不标记完成。操作与边界见 [密钥轮换](key-rotation.md)。

轮换协议补验：`TestRealJWKSOfflineRotationAndVerifierCache` 在独立 PostgreSQL 与真实 HTTP 端点下通过 race 检查。实际 ID Token 经过普通/紧急轮换；Go coreos 验证库未知 kid 刷新、旧公钥保留、JWKS 无私钥、全新验证器拒绝已移除键及断网拒绝均已验证。旧验证器的缓存仍信任旧键也被明确测试；TS/Python 缓存联调与自动紧急撤销继续待完成。

TS/Python 轮换缓存补齐：新增签名事件轮换契约测试，验证冷却期、未知 kid、旧键缓存和离线拒绝；新增受信运维缓存清理 API，清理后拒绝已移除公钥并接受当前公钥。此处为模拟 HTTP 契约，不冒充真实多进程轮换验收；现有项目会话撤销与全局事件仍待完成。

死信运维 API：新增近期 MFA 管理员专用 outbox 分页状态列表与单事件重试，POST 使用既有 Origin/CSRF 边界。重试保留 ID/载荷、拒绝有效租约和已投递记录、与审计原子提交；真实 PostgreSQL 并发及故障回滚测试通过，服务 HTTP 回归和 go vet 通过。管理控制台、投递历史明细及端到端管理员界面仍待完成，不能计作死信运维全量交付。

React 控制台首版：`web/` 接入 `/console/` 可选静态挂载，安全中心与管理员 API 有可操作页面。构建 TypeScript/Vite 通过；Go 静态路由与安全头测试通过；Chrome 在桌面/手机宽度检查未登录与模拟管理员页面无横向溢出。控制台启用时直接 `/login` 成功后返回页面，未启用时维持原 JSON 行为。当前没有真实浏览器登录/MFA/管理流程端到端验收，公开邀请/恢复界面、全部应用与服务身份管理、分页、部署仍待完成，不勾选 P6。

公开邀请注册/密码恢复页面补齐：React 表单支持手动一次性凭证或链接查询参数，页面初始化立即清除查询参数；成功后提示独立登录。真实 Go/PostgreSQL HTTP 契约通过，验证邀请重放拒绝、恢复沿用原身份并撤销旧会话、旧密码失效与新密码登录。Chrome 模拟 API 检查两种表单提交和成功提示、邀请页桌面/移动端布局。完整浏览器到真实后端及邮件/私密凭证交付仍待验收。

真实浏览器入门闭环补验（2026-09-23）：新增可选 Chrome 契约 `TestConsoleAgainstRealBrowser`，在隔离 PostgreSQL/真实 Go HTTP/已构建 React 页面中通过邀请注册 → 登录 → 会话页 → 退出 → 恢复 → 旧密码拒绝 → 新密码登录。浏览器发现并修复登录页 no-referrer 造成 `Origin: null` 的原生表单失败；改为 same-origin 后仍通过跨站拒绝测试。完整服务端 race、go vet、web npm ci/build 均通过。管理员 MFA、项目绑定及生产部署浏览器验收仍未完成。

管理员浏览器与应用密钥进展：真实 Chrome/Go/PostgreSQL 测试完成 TOTP 开通，证明启用前管理 API 拒绝、启用后可登记应用和签发邀请；创建后的密钥轮换在页面通过，并验证新密钥只在内存中展示。004 迁移支持上一把密钥有界交接期，默认 10 分钟、最多 1 小时；到期自动拒绝旧密钥，连续轮换覆盖更旧凭证。真实 PostgreSQL 测试覆盖权限、到期、公开客户端拒绝及审计失败回滚。服务端 race、go vet 和前端构建通过。服务身份和多实例凭证分发/部署演练仍待完成。
