# 单主机 HTTPS 部署样例

`compose.prod.yaml` 将 PostgreSQL、FastCAS 置于私有 Compose 网络，仅由 Caddy 发布 80/443。Caddy 为域名申请并续期公开证书；`FASTCAS_ISSUER` 必须是浏览器实际访问的 HTTPS origin，和已注册客户端的 issuer 完全一致。域名 A/AAAA 记录、入站 80/443、防火墙及备份存储需在部署主机上准备。此文件是单主机部署样例，跨主机高可用、监控告警和异地恢复仍需另行设计。

在 `FastCAS/` 目录创建私有环境文件（参考 `compose.prod.env.example`），使用强随机且 URL 安全的数据库密码，并另生成至少 32 字节的 `FASTCAS_PROXY_SECRET`，例如 `openssl rand -hex 32`。不要提交环境文件。以下以 `.local/production.env` 为例；首次启动在空数据卷上执行初始化，后续重启不要再运行 `init-keys` 或 `bootstrap`：

```bash
docker compose --env-file .local/production.env -f compose.prod.yaml build fastcas
docker compose --env-file .local/production.env -f compose.prod.yaml up -d postgres
docker compose --env-file .local/production.env -f compose.prod.yaml run --rm fastcas init-keys
docker compose --env-file .local/production.env -f compose.prod.yaml run --rm fastcas migrate
FASTCAS_ADMIN_EMAIL='admin@example.com' FASTCAS_ADMIN_PASSWORD='替换为强密码' \
  docker compose --env-file .local/production.env -f compose.prod.yaml run --rm \
  -e FASTCAS_ADMIN_EMAIL -e FASTCAS_ADMIN_PASSWORD fastcas bootstrap
docker compose --env-file .local/production.env -f compose.prod.yaml run --rm fastcas doctor
docker compose --env-file .local/production.env -f compose.prod.yaml up -d fastcas caddy
curl -fsS https://auth.example.com/health/ready
curl -fsS https://auth.example.com/.well-known/openid-configuration
```

替换示例域名和管理员地址。Caddy 数据卷须持久保存证书与 ACME 账户。备份与恢复必须成对处理数据库和 FastCAS 密钥卷，见[恢复演练](compose-recovery.md)；备份 Caddy 数据卷可避免恢复后重新签证。生产实例不要设置 `FASTCAS_ENV=development`。FastCAS 根据固定的 HTTPS issuer 设置安全 Cookie 和校验浏览器 Origin，不依据客户端传入的转发头改变 issuer。

Caddy 在转发时覆写 `X-FastCAS-Client-IP` 并附上共享密钥。FastCAS 只在密钥匹配且地址合法时将其用于登录、设备验证和一次性凭证兑换的按 IP 限流；其余情况使用 TCP peer。不要把密钥暴露给浏览器，也不要把 FastCAS 的 8900 端口发布到公网。此样例假定 Caddy 直接面对用户；若前面还有 CDN 或负载均衡，需先明确其可信代理配置，否则 Caddy 看到的仍是上一级代理地址。

对事件与注销通知，每分钟由监控系统在运行容器中执行一次：

```bash
docker compose --env-file .local/production.env -f compose.prod.yaml exec -T fastcas fastcas check-outbox
```

该命令输出不含身份、事件 ID 或令牌的聚合 JSON；默认在最老未投递事件超过 30 秒、存在死信或最近一小时 P95 完成时延超过 30 秒时返回非零退出码。监控系统应对连续失败发出告警，并由管理员在控制台查看失败投递及逐次历史；可按实际 SLO 调整 `--max-pending-age`、`--max-dead`、`--max-p95-delivery` 和 `--window`。`/health/ready` 仅证明服务和数据库可用，不因单个接收方故障让 FastCAS 被负载均衡器摘除。每分钟维护任务分批删除成功投递超过 90 天的事件与尝试记录；待处理和死信不自动删除。

2026-09-23 本地隔离演练：构建生产镜像，初始化空 PostgreSQL/密钥卷，采用 Caddy `tls internal` 在临时端口完成 HTTPS 代理；`/health/ready` 返回 200、discovery issuer 与配置一致、登录页设置 `Secure` Cookie，HTTP 请求得到 308，正式 Caddyfile 通过 `caddy validate`。演练后已删除临时容器与卷。内部测试证书及非标准端口不等于公网 ACME 验证。

代理头另经本地容器演练：客户端故意提交伪造的 `X-FastCAS-Client-IP: 203.0.113.77` 和代理密钥，后端实际收到 Caddy 覆写的 Docker 网关地址及配置密钥；临时容器与网络随后已删除。
