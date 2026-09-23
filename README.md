# FastCAS

状态：正在按规划开发，尚未完成全量交付。调研及开发日期：2026-09-22。

已建立 Go/PostgreSQL 服务和三语言 SDK 的核心登录、已有账号绑定链路；参见 [开发与测试](docs/development.md)、[三语言示例与兼容矩阵](examples/README.md)、[全项目验收入口](docs/verification.md)、[本地发布产物](docs/release.md)、[单主机 HTTPS 部署样例](docs/production-compose.md)和[完整实施清单](docs/implementation-status.md)。部署后可用 `fastcas check-outbox` 将事件积压、死信和投递时延接入监控告警。规划文档保留为目标规格，不能当作已实现功能清单。

FastCAS 是 FastR-D 工具生态的**可选统一身份服务**，后端使用 Go。项目保留自己的账号、登录、注册和业务权限；用户既可以为已有项目账号完成 FastCAS 认证，也可以选择使用 FastCAS 登录。未绑定用户仍可正常使用项目允许的本地能力。

这里的“认证”指证明用户同时控制项目账号和 FastCAS 身份，建立可撤销的绑定；不表示实名、学术资质或组织成员资格认证。FastCAS 不代管项目密码，不自动合并同邮箱账号。

## 阅读顺序

1. [项目现状与代码依据](docs/planning/01-project-research.md)
2. [产品形态、认证与登录流程](docs/planning/02-product.md)
3. [产品比较、技术选型与架构](docs/planning/03-architecture.md)
4. [API、身份模型与三语言 SDK](docs/planning/04-api-sdk.md)
5. [七个项目接入与数据迁移](docs/planning/05-integration.md)
6. [实施阶段、验收与风险](docs/planning/06-delivery.md)

## 建议决策

| 事项 | 建议 |
| --- | --- |
| 账号归属 | 项目账号独立；FastCAS 通过绑定关系关联，项目 ID 和数据所有权不变 |
| 登录 | 原登录方式与 FastCAS 登录并存；注册政策由各项目自己决定 |
| 协议 | OIDC Authorization Code + PKCE；传统 CAS 协议暂不实现 |
| 后端 | Go 1.26 最新修订版起步，chi + zitadel/oidc + PostgreSQL；选型须通过协议验证阶段 |
| 界面 | React + TypeScript；统一登录页、个人安全中心、开发者/管理员控制台 |
| SDK | TypeScript（浏览器与服务端分入口）、Go、Python（同步与异步） |
| 试点 | FastWrite 验证 TS 与已有账号绑定，FastTask 验证 Go，FastRead 验证 Python |
| 服务与本机工具 | FastInsight 使用服务身份；FastLabs 保留本机模式，按需关联操作人 |
| 发布边界 | 当前进行本地实现与隔离数据库验证；尚无远程仓库创建、包发布或生产部署 |

规划中的版本与部署参数应结合开发记录阅读。生产数据、真实部署配置和生产运行效果尚未验证。
