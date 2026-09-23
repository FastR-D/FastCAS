# 三语言服务身份示例

三份 CLI 演示同一条服务授权链：通过保密客户端凭证取得限定 scope 的访问令牌，按目标 resource 验证 issuer/签名/受众/scope 与服务身份，再向中心查询令牌是否仍有效。输出只有客户端 ID、受众、scope 和状态；不打印令牌或密钥。示例中的事务存储明确拒绝交互登录，不能复制到用户登录接入。

先在 FastCAS 管理页面登记仅允许 `client_credentials` 的服务身份，设置 scope（如 `insight:publish`）和 resource（如 `research-api`）。将一次性返回的密钥保存在进程专用的环境配置中，不放入前端。

在 `FastCAS/` 目录配置 `FASTCAS_ISSUER`、`FASTCAS_CLIENT_ID`、`FASTCAS_CLIENT_SECRET`、`FASTCAS_AUDIENCE`、`FASTCAS_SCOPE`，再运行：

```bash
go run ./examples/service-tokens
node examples/service-tokens/node.mjs
PYTHONPATH=sdk/python/src .venv/bin/python examples/service-tokens/python.py
```

仅在明确的本机开发环境设置 `FASTCAS_ALLOW_LOOPBACK_HTTP=true`。资源服务仍需检查本地 ACL 和收件人范围；服务令牌不代表任意用户。生产进程应缓存短期令牌并处理过期，不能把上面的演示命令当作每请求的授权中间件。
