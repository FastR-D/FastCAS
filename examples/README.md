# SDK 示例与兼容矩阵

[`service-tokens/`](service-tokens/README.md) 包含可直接运行的 Go、Node 和 Python 服务身份示例，展示短期凭证签发、资源受众与 scope 验证，以及中心撤销查询。示例在真实 FastCAS/PostgreSQL 契约中逐一运行，停用服务身份后三种实现均失败关闭。它不处理用户登录，也不授予收件人或业务 ACL。

现有项目提供更完整的可选登录/认证参考实现：

| 运行时 | 实际接入位置 | 边界 |
| --- | --- | --- |
| TypeScript / Bun | [FastWrite](../../FastWrite/apps/server/src/auth/fastcas-service.ts) | 原本地账号、旧 OIDC 与 FastCAS 并存 |
| TypeScript / Node | [FastResearch](../../FastResearch/server/fastcas-service.mjs) | Key 登录与稳定账号、内容归属分离 |
| Go / Gin | [FastTask](../../FastTask/internal/httpapi/fastcas.go) | 本地管理员、设备令牌和 FastCAS 来源隔离 |
| Python / FastAPI | [FastRead](../../FastRead/backend/app/web/fastcas_routes.py) | 本地密码、旧 Research 票据与 FastCAS 并存 |
| Python / HTTPServer | [FastNews](../../FastNews/fastnews_cas.py) | 本地账号与个人内容保持独立 |
| Python / CLI | [FastInsight](../../FastInsight/scripts/service_auth.py) | 服务身份只代表投递应用，收件人单独校验 |
| Python / 本机 | [FastLabs](../../FastLabs/fastcas_pairing.py) | 设备码与本机二次确认，不赋予远程任务权限 |

当前本地验证组合为 Go 1.26.8、Node 22、Bun 1.4、Python 3.10（SDK）及 Python 3.13（FastRead/FastNews）。服务与三语言 SDK 当前版本为 0.1.0，尚未发布远程包或 tag；项目使用本地路径依赖。实际发布前仍需在目标运行环境重新执行[全项目门禁](../docs/verification.md)，不能把本表当作跨版本兼容承诺。
