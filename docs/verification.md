# 可重复验收入口

本地全项目契约入口为 `make verify-contracts`。在 FastCAS 与 FastWrite、FastTask、FastRead、FastResearch、FastNews、FastInsight、FastLabs 位于同一父目录时执行。需要独立 PostgreSQL 测试库（`FASTCAS_TEST_DATABASE_URL` 或权限受限的 `.local/test-database-url`）、Go 1.26.8、Node 22、Bun、Python SDK 虚拟环境、FastRead 与 FastNews 虚拟环境（FastRead 含项目已锁定的 `uvicorn==0.34.0` 与 `python-multipart==0.0.20`）、Google Chrome，以及各项目契约测试所需的已安装依赖。脚本先检查 OpenAPI，构建 FastCAS SDK/控制台及 FastWrite、FastResearch、FastTask、FastRead 前端，运行 Go/Python SDK 测试及七项目账号相关本地回归，再以 race 模式启动真实 FastCAS/PostgreSQL、七项目进程和浏览器契约。单独运行 `make verify-project-regressions` 可只执行七项目回归；它不需要 PostgreSQL 测试库。

`scripts/check_contract_results.py` 逐项要求 28 个真实提供方/浏览器测试显示 `pass`；`skip`、缺失或失败均使命令失败，并打印失败案例的实际输出。它包括五个账号项目的登录/绑定与身份状态事件、Insight 受限投递、Labs 设备授权、News→Research 委托读取、三语言 SDK 与服务身份示例、控制台与 FastWrite、FastResearch、FastTask、FastRead、FastNews、FastLabs 项目浏览器及服务身份生命周期。各项目原本地登录和内容边界在对应契约内检查。2026-09-23 本地全入口最新通过 28/28；FastWrite 浏览器案例在故意延迟回调刷新 1.2 秒后，验证本地登录、绑定、新浏览器 CAS 登录、原用户和项目保持及解绑来源隔离；FastLabs 浏览器案例验证中心批准后仍需本机确认，设置页刷新修正后以 race 模式连续运行五次通过。FastNews 案例现在从隔离临时目录生成七个真实报告页面，逐页验证账号入口并从栏目页点击进入本地登录，接着完成现有账号认证与 FastCAS 登录。控制台浏览器案例还检查死信投递历史及无正文泄露。此前 FastResearch、FastRead、FastNews 等项目的浏览器案例也已分别完成重复验收。它不是生产域名、真实用户数据、跨地域恢复或容量压测的替代。

七项目回归阶段覆盖 FastWrite 13 项、FastRead 17 项、FastResearch 8 项、FastNews 16 项、FastInsight 3 项、FastLabs 本机测试及 FastTask 三个核心包的 race 测试。2026-09-23 将它们接入后，完整 `make verify-contracts` 最新通过 28/28；本地门禁因此同时要求项目内部账号回归和跨项目协议契约通过。

`.github/workflows/core-sdk.yml` 为 FastCAS 单仓库 CI：使用独立 PostgreSQL 16 服务，构建 React/TS SDK，运行 Python/Go SDK 测试及 Go race/vet，并强制确认 11 个核心真实协议/SDK/示例契约没有被跳过。核心测试后运行 `make release`，隔离安装三语言 SDK，生成服务、Web、SDK 和 OpenAPI 本地产物及校验和，并上传 CI artifact；本地构建和双次一致性检查见[发布说明](release.md)。七项目集成 CI 需要将对应仓库、锁定依赖和浏览器运行环境纳入同一工作区；当前 FastCAS 仅为本地目录且其他项目接入变更仍在工作区，公开仓库检出不能代表这些改动。先由本地全项目入口验收，不能把单仓库 CI 的通过视为七项目发布门禁完成。

管理员控制台的真实 Chrome 契约还在每类至少 105 条 PostgreSQL 记录下检查身份、应用、服务身份与审计的“加载更多”，并拒绝非法审计游标。该流程继续经过邀请、恢复、登录、MFA、死信记录、密钥轮换与服务身份生命周期；页面不依赖模拟列表数据。
同一管理员流程打开开发者接入页，从真实 discovery 读取 issuer，检查已登记应用及后续配置页，并确认此前轮换的客户端密钥没有出现在向导正文。

同一 Chrome 契约还在开通 MFA 后轮换恢复码，并检查页面仅在当次展示新码；真实数据库测试验证旧码立即失效、异常事务回滚后旧码保持有效，以及非近期双因素会话不能轮换。
同一 Chrome 契约还让新邀请的成员在安全中心自行改密，验证旧密码被拒绝且新密码可登录。真实数据库测试进一步覆盖已启用 MFA 时密码与验证码/恢复码的双重校验、所有中心会话和待完成授权的撤销、项目退出通知与审计；项目本地密码不参与修改。
管理员签发的密码恢复另有真实数据库回归：审计插入失败时事务回滚，恢复凭据可重试，旧密码和会话保持有效；成功后待完成授权码及已批准设备授权失效，项目退出通知仅针对活跃关联发送。
失设备场景另由真实 Chrome 契约完成管理员签发一次性 `mfa_reset` 凭证、持有人用当前 FastCAS 密码兑换、旧管理员会话退出、重新登录后管理 API 在新 MFA 开通前返回 403。真实 PostgreSQL 核心测试还确认旧恢复码、已批准授权码与设备授权失效，项目退出通知入队，审计失败则事务整体回滚。此流程仍依赖可信管理员与私密凭证交付。

身份状态事件契约在真实项目接收端记录 outbox 创建到项目确认的时间。FastTask 案例故意让接收方首次返回 HTTP 503，并验证自动重试后 CAS 来源会话和刷新令牌失效、本地会话保留；race 模式连续三次到达时间为 2.06–2.18 秒。FastWrite、FastRead、FastResearch、FastNews 的正常投递也都核对到达时间并要求低于 30 秒。此为隔离本机测量，不覆盖生产网络和容量负载。

可单独运行 `make verify-capacity` 测量真实 PostgreSQL/HTTP 下 64 个独立账号与独立环回来源地址的授权码登录、同意和刷新突发，以及签名 outbox 的积压、503 故障和退避恢复（`-race -parallel 8`）；缺少独立数据库配置会直接失败，不把跳过当通过。2026-09-23 单独运行登录项：64/64 完成，登录 p50 1.83 秒、p95 2.41 秒，刷新 p50 95 毫秒、p95 433 毫秒，突发阶段约 15.25 秒。单独运行积压项：256/256 次唯一签名通知全部成功，无死信，8 worker 投递阶段约 213 毫秒、到达 p95 约 205 毫秒。故障项：128 个通知先收 503，再经过真实 2 秒退避全部成功，256 条尝试历史齐全，单项恢复阶段约 2.21 秒。整套入口并行运行时三项仍通过，但共享机器上的恢复阶段为 7.84 秒。创建测试账号的时间不计入突发阶段；这些值是当前机器的诊断基线，不是生产 SLO。生产 TLS/代理、持续数百用户、长时间事件积压与跨项目撤销压力仍需独立测量。单账号/同来源密码登录存在每 15 分钟 12 次的安全限流，不应通过提高该限制来伪造容量结果。
