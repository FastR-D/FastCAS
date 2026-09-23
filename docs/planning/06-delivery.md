# 实施阶段、验收与风险

## 实施顺序

以下为任务拆分和粗略工作量，不是交付承诺。按 1 名熟悉这些项目的开发者估算，共约 45–70 个工程日；包含认证、三个 SDK、七项目改造和回归，实际随生产数据与部署差异调整。若使用现成 IAM，协议部分工作可减少，但项目本地账号与绑定改造仍存在。

| 阶段 | 依赖 | 产出 | 粗估 | 完成标准 |
| --- | --- | --- | --- | --- |
| P0 协议/绑定验证 | 本规划 | ADR、依赖锁定、两个示例 RP、绑定与撤销原型 | 3–5 日 | TS/Bun 与 Python 或 Go 互通；原子消费、轮换、重放、失败恢复通过；确定协议库或 Hydra fallback |
| P1 FastCAS 核心 | P0 | Go 服务、PostgreSQL migration、登录/邀请/恢复、应用管理、绑定、安全中心、审计 | 8–12 日 | 可独立部署；双端证明、冲突、解除、来源隔离与 MFA 管理流程可测 |
| P2 三 SDK 首版 | P0/P1 契约 | TS/Go/Python 包、示例、契约 fixtures、版本矩阵 | 5–8 日 | 各运行时通过相同正反例，支持注入 store，客户端不泄露 secret |
| P3 账号型试点 | P1/P2 | FastWrite、FastTask、FastRead 接入 | 9–14 日 | 本地入口与 CAS 并存；原资源归属不变；三语言各有真实项目验证 |
| P4 门户与资讯 | P3 + 数据映射 | Research stable accounts、News 本地入口与内容 store、旧票据兼容 | 8–12 日 | 不依赖 CAS 的链路完整；无同名自动合并、无内容丢失、无共享 Cookie |
| P5 服务/本机 | P2/P4 + device/exchange 验证 | Insight 服务身份、Labs 可选关联、跨项目授权 | 5–8 日 | 不扩大服务权限，不影响本机离线，不需要公网本机回调 |
| P6 发布与迁移演练 | 全部 | 可复现发布、升级/回滚、备份恢复、集成回归与部署手册 | 7–11 日 | 七项目可选接入验收通过；故障/撤销时限经过实测；产物含 SDK 文档与样例 |

实现顺序首先证明“本地用户绑定后仍进入同一账号”，再扩展到跨项目服务调用。不能先全量替换各项目登录页再补迁移。

## 推荐目录结构（未来实现）

```text
FastCAS/
  cmd/fastcas/              # serve / migrate / bootstrap / doctor
  internal/
    identity/ application/ linking/ session/ policy/
    protocol/oidc/ storage/postgres/ events/ audit/ httpapi/
  migrations/
  web/                     # 登录、个人安全中心、管理
  sdk/typescript/          # browser / server / adapters
  sdk/go/                  # 独立 go.mod
  sdk/python/              # core / sync / async / fastapi
  api/                     # 管理和绑定 API OpenAPI
  contracts/               # 共享正反例，不包含真实凭证
  examples/                # Node/Bun、Go、FastAPI/HTTPServer
  deployments/             # compose、systemd、TLS 样例
  docs/planning/           # 本轮已交付内容
```

建议后续各项目分别提交小范围变更：模型/本地入口 → provider → 绑定 UI → 撤销/会话 → 集成测试。项目间以 SDK 版本与契约依赖，不用跨仓库复制工具函数。

## 验收矩阵

### 产品必要条件

1. FastCAS 未配置时，每个项目原入口不发中心请求；原有本地/Key/本机模式继续工作。
2. 新项目账号注册或管理员开户不隐式创建 FastCAS 身份；注册政策仍由项目决定。
3. 绑定前后 local user ID、workspace、论文、任务、ACL、作者关注均不改变。
4. FastCAS 登录找不到绑定时有明确选择；同名/同邮箱不自动合并；取消流程不会留下可登录的半成品关系。
5. 解绑不删除本地账号；CAS-only 账号有恢复措施后才能解除最后登录方式。
6. CAS 全局注销只影响 CAS 来源会话；仅做绑定的本地会话保留。
7. 项目禁用账号可阻断所有登录来源；FastCAS 禁用不会自动禁用独立项目账号。
8. FastLabs 离线与 FastInsight 原脚本模式可用，接入不会扩大执行/投递权限。

### 协议、绑定与授权

| 测试 | 期望 |
| --- | --- |
| 错 state/nonce/PKCE、未注册回调、开放重定向 | 拒绝；不创建账号、绑定或会话 |
| callback 重放、code 双并发消费 | 最多一次成功 |
| 绑定过程中切换本地用户/浏览器、过期本地认证 | 拒绝或重新验证；不更改原账号 |
| 两个事务争抢同一账号或 subject | 唯一约束保证至多一个 active；失败端可恢复 |
| prepare 后宕机、activate 响应丢失 | 幂等 reconcile，不凭 pending 关系登录 |
| 延迟/重复/乱序撤销事件 | 版本单调；已撤销关系不复活 |
| 非预期 issuer/audience/alg/type、ID Token 用作 API token | 拒绝 |
| client credentials + 任意 represented_user_id | 不能获得用户权限 |
| 错 resource/scope、target 用户未绑定/被项目禁用 | 跨项目访问拒绝 |
| refresh 重放、并发正常 refresh、轮换与旧 key | 区分攻击与串行刷新；没有无限信任旧凭证 |
| CSRF、绑定 login CSRF、跨端口 Cookie、伪造转发头 | 不串号、不错误信任请求来源 |
| FastWrite room grant、FastTask Device Token | 维持原来的资源/只读边界 |

协议内核适配要有实际 PostgreSQL 事务测试，不能只用 mock 证明一次性消费。SDK 使用同一组错误 token 与绑定冲突 fixtures；服务访问测试不携带生产密钥。

### 故障和运维

- 中心断网：本地来源会话与本地登录不受影响；CAS 登录失败有可读提示；CAS 来源会话按已定义的 5 分钟有效性缓存上限失效。
- JWKS 轮换：缓存命中/未知 kid 刷新/密钥泄露撤销分别验证，避免网络异常放行。
- 注销 webhook 失败：重试、限次告警、轮询补偿；测量撤销实际时延而非只看接口 200。
- 迁移：备份恢复后账号计数、资源外键、内容 hash 与映射比对；回滚不会复活已撤销凭证。
- 初始容量假设为单团队至数百用户、七类项目，压测以登录突发、刷新突发与事件积压为主；不以猜测吞吐量作为发布指标。
- 登录协议不得采集模型、飞书或 GitHub 业务 secret；日志/浏览器 URL/错误页中无敏感凭证。

## 已有测试的复用

本轮未运行项目测试，因为未修改业务代码。后续实现应使用：

- FastWrite：Bun 单测、typecheck、构建及真实浏览器认证/协作回归。
- FastTask：Go 单测/race/vet、HTTP/OpenAPI 契约、前端测试与构建；新 SDK 前先完成 Go 升级回归。
- FastRead：Web auth/workspace 测试、前端类型检查和构建、跨用户访问与 worker 数据归属回归。
- Research/News：补齐原 Key/SSO 链路、本地链路与新 CAS 的集成测试，使用独立实例数据目录。
- Insight/Labs：保留脚本和本机任务测试；真实飞书发送、CLI 执行不作为普通测试副作用。

## 主要风险与应对

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 将认证误解为强制统一账号 | 破坏用户已明确要求 | 独立配置、local 会话来源、本地无网络调用作为硬验收 |
| CAS identity 与本地 ID 混用 | 数据串号/丢失 | 稳定映射表、双端证明、逐项目数据比对 |
| Research Key 删除等于删用户 | 凭证轮换损害内容 | 先分离 account/credential/content，再加 CAS |
| FastRead 看似有密码函数但路由禁用 | 本地可用性承诺不成立 | 修改当前 Web gate/路由并实测直访 |
| News 对 Research 内容强耦合 | 登录成功但个人功能不能用 | PersonalContentStore、local 默认、显式连接与单主写入 |
| 绑定两边事务中断 | 误认证或重复绑定 | prepared/active 分离、唯一约束、幂等与 reconcile |
| 本地会话无限继承 CAS 授权 | 撤销失效 | 会话来源、sid、版本、backchannel 和有效性上限 |
| SDK 运行时冲突 | Bun/Python/Go 接入失败 | P0/P2 固定基线；Insight 升级可选，Task 工具链升级独立 |
| 为本机工具开放公网入口 | 扩大执行面 | Device flow + 本地确认；保持 loopback 与原任务审批 |

## 可直接采用的默认决定与仍待验证项

默认采用：可选 FastCAS；本地账号不迁走；认证等于可撤销控制权绑定；OIDC 主协议；单实例一对一绑定；邀请制 FastCAS 注册；本地注册遵循原项目政策；所有非账户型工具按自身形态接入。

需要在对应阶段确定、但不阻塞本轮规划的事项：

- P0：OIDC 库固定版本及授权存储原子性；SDK 运行时矩阵；传统 CAS 是否有真实消费者（目前未发现必要性）。
- P1：正式域名、部署实例 ID、邀请与恢复通道、管理员 bootstrap/MFA、密钥托管与运维目标。
- P3/P4：真实用户/Key 重复情况、存储模式、已有公开注册政策、原认证 cookie/前端会话如何兼容。
- P5：需要哪些具体跨项目服务调用与收件人范围；不泛化为全局服务管理员权限。

上述默认决定均已写成可执行规划；本轮交付不要求先回答这些问题，也不据此启动实现或生产迁移。
