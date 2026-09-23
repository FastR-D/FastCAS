# 本地容器部署与恢复演练

`compose.yaml` 是仅绑定 `127.0.0.1:8900` 的单实例开发部署。单主机 HTTPS 反向代理样例见[生产 Compose](production-compose.md)；可信代理头策略、独立密钥管理、监控与多实例运维仍需完善。以下命令在 `FastCAS/` 目录执行；`FASTCAS_DB_PASSWORD` 应为强随机且可用于数据库 URL 的字符，勿提交到版本库。Issuer 一旦被客户端登记，不可在同一环境中随意改变。

```bash
export FASTCAS_DB_PASSWORD='替换为本地专用密码'
export FASTCAS_ISSUER='http://127.0.0.1:8900'
docker compose build fastcas
docker compose up -d postgres
docker compose run --rm fastcas init-keys
docker compose run --rm fastcas migrate
FASTCAS_ADMIN_EMAIL='admin@example.test' FASTCAS_ADMIN_PASSWORD='替换为强密码' \
  docker compose run --rm -e FASTCAS_ADMIN_EMAIL -e FASTCAS_ADMIN_PASSWORD fastcas bootstrap
docker compose run --rm fastcas doctor
docker compose up -d fastcas
curl -fsS http://127.0.0.1:8900/health/ready
```

首次管理员登录后需开通 TOTP，方可操作管理控制台。`init-keys` 只用于空密钥卷，重跑会拒绝覆盖。应用注册与回调地址按实际部署环境填写。

## 一致性备份

先停止**所有** FastCAS 签发及事件投递实例，再备份 PostgreSQL 和 `/state/keys`，两份文件视为一个恢复点。不要在应用继续写库期间分别拷贝它们。下例使用当前 Compose 项目；备份目录权限为 0700，文件为 0600，应加密后异地保存并周期演练。

```bash
docker compose stop fastcas
umask 077
mkdir -p .local/backup
docker compose exec -T postgres pg_dump -U fastcas -d fastcas -Fc -f /tmp/fastcas.dump
docker compose cp postgres:/tmp/fastcas.dump .local/backup/database.dump
docker compose run --rm -T --no-deps --entrypoint tar fastcas -C /state -czf - keys > .local/backup/keys.tar.gz
sha256sum .local/backup/database.dump .local/backup/keys.tar.gz > .local/backup/SHA256SUMS
docker compose up -d fastcas
```

## 向全新环境恢复

隔离新项目和空卷演练，或在正式事故中准备全新的数据库/密钥卷。**不要对仍在服务的数据库执行 `pg_restore`。** 全程停止签发与投递。恢复前核对两份文件哈希、PostgreSQL 版本与密钥归属；新环境先启动空数据库，等待健康后导入。

```bash
export FASTCAS_DB_PASSWORD='替换为目标环境专用密码'
docker compose -p fastcas-restore up -d postgres
docker compose -p fastcas-restore exec -T postgres sh -c 'until pg_isready -U fastcas -d fastcas; do sleep 1; done'
docker compose -p fastcas-restore cp .local/backup/database.dump postgres:/tmp/database.dump
docker compose -p fastcas-restore exec -T postgres pg_restore -U fastcas -d fastcas --no-owner --no-acl /tmp/database.dump
docker compose -p fastcas-restore build fastcas
docker compose -p fastcas-restore run --rm -T --no-deps --entrypoint tar fastcas -C /state -xzf - < .local/backup/keys.tar.gz
docker compose -p fastcas-restore run --rm fastcas doctor
docker compose -p fastcas-restore run --rm fastcas migrate
docker compose -p fastcas-restore run --rm fastcas fence-restore
docker compose -p fastcas-restore run --rm fastcas rotate-keys --emergency
docker compose -p fastcas-restore run --rm fastcas doctor
docker compose -p fastcas-restore up -d fastcas
curl -fsS http://127.0.0.1:8900/health/ready
```

`fence-restore` 在一个事务中撤销快照里的会话、授权码、访问/刷新令牌、设备授权、设备安装关联、一次性凭证、委托同意和旧绑定；清除旧待投递事件，并为已登记项目重新排入退出与绑定撤销通知。它不删除用户或项目本地账号。FastLabs 在下次联网状态检查时发现安装已撤销；若恢复快照里根本没有这台安装，中心拒绝旧安装凭据，本机也会清除旧关联并允许重新配对。用户需要重新登录并重新证明本地账号所有权后才能再绑定。该命令可重复执行，但重复执行会重新排队通知。紧急轮换只替换签名密钥，保留 `encryption.key` 以解密 MFA 秘钥；应按[密钥轮换说明](key-rotation.md)协调接入方清除 JWKS 缓存。未刷新缓存的离线验证器仍可能在旧令牌剩余有效期内接受旧签名，因此在缓存失效和通知投递完成前不要恢复外部流量。旧快照后的业务数据无法从旧备份找回，应按 RPO 评估并与项目数据源核对。

2026-09-23 实测：两个隔离 Compose 项目分别使用新卷；备份 PostgreSQL 自定义格式与密钥包，在第二项目恢复、执行 `doctor`、`fence-restore`、紧急轮换后，`/health/ready` 返回 200、控制台返回 200、JWKS 仅有新公钥，管理员记录和恢复审计记录存在。数据库测试另覆盖了快照会话、令牌、绑定撤销及重复运行通知。此演练未覆盖生产反向代理、真实项目会话或跨主机异地恢复。
