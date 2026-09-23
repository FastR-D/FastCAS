# 本地开发与当前实现

FastCAS 正在开发，完整功能尚未交付。核心 OIDC、账号、MFA、绑定与撤销事件、设备授权、受限 token exchange、签名轮换、全局退出、React 控制台及多个项目的可选接入已有实现和专项验证；生产部署与全量产品验收仍未完成。以 [实施清单](implementation-status.md) 为完成状态依据；本地容器启动与恢复见[容器演练](compose-recovery.md)。

## 开发环境

Go 模块锁定 `go1.26.8`；首次 Go 命令自动下载工具链。需要 PostgreSQL 16+、Node 22+、Bun（跨运行时测试）、Python 3.10+、uv。

数据库测试每次创建独立 schema 并在结束后删除，只使用专用测试库。设置 `FASTCAS_TEST_DATABASE_URL`，或把连接串放入权限 0600 的 `.local/test-database-url`。本任务已启动专用 `fastcas-test-db` Docker 容器，动态绑定回环端口；没有访问其他项目数据库。

```bash
# FastCAS 目录下
cd sdk/typescript
npm ci --ignore-scripts
npm test
cd ../..
uv venv --python python3.10 .venv
uv pip install --python .venv/bin/python -e sdk/python
.venv/bin/python -m unittest discover -s sdk/python/tests -p 'test_*.py' -v
FASTCAS_SDK_CONTRACT=1 go test -race ./...
go vet ./...
go build -trimpath -o bin/fastcas ./cmd/fastcas
```

`FASTCAS_SDK_CONTRACT=1` 会在 Go 服务的真实 HTTP 测试中运行 Node、Bun、Python SDK；Go SDK 契约测试默认运行。测试库未配置时数据库测试会标注 SKIP，不可把这种运行作为完整通过；发布 CI 必须提供数据库并开启 SDK 契约测试。

全部七项目及浏览器的严格契约入口为 `make verify-contracts`。它检查 28 个必需测试实际通过，不接受跳过；准备条件与边界见[可重复验收入口](verification.md)。`make release` 构建并隔离烟测本地发布产物，见[发布说明](release.md)。单仓库核心 SDK CI 位于 `.github/workflows/core-sdk.yml`，不代表七项目生产发布完成。

## 启动开发服务

先按 `.env.example` 配置自己的环境变量（Go 程序不自动读取该文件）。不要直接使用示例密码或把真实凭证提交到仓库。

```bash
go run ./cmd/fastcas init-keys
go run ./cmd/fastcas migrate
# 仅首次管理员开户，密码通过 FASTCAS_ADMIN_PASSWORD 环境变量传入
go run ./cmd/fastcas bootstrap
# 应用密钥通过 FASTCAS_CLIENT_SECRET 环境变量传入
go run ./cmd/fastcas register-client --file examples/application.json
go run ./cmd/fastcas doctor
go run ./cmd/fastcas serve
```

`init-keys` 只创建新的密钥目录，拒绝覆盖已有文件。生产 issuer 必须 HTTPS；只有显式 `FASTCAS_ENV=development` 才允许回环 HTTP。设置 `FASTCAS_UI_DIR` 可挂载已构建的 React 控制台；Compose 镜像已包含它。

首次管理员登录后需通过账号 MFA 接口完成 TOTP 注册，才能访问管理员 API；管理员密码本身不能访问管理接口。邀请和密码恢复由管理员签发一次性凭证，凭证只返回一次且需私下交付给目标用户；尚未实现邮件自动投递。

## 已通过的核心测试

- PostgreSQL 并发消费授权码，12 个调用仅一次成功。
- 真实 HTTP 授权码/PKCE、ID Token 签名、userinfo、刷新轮换和重放拒绝。
- 服务令牌身份与 audience 隔离；ID Token 不可充当 API access token。
- 用户双端确认前拒绝绑定；prepared 不可登录；绑定唯一性、幂等、版本与撤销不可复活。
- 过期 prepared 预留可回收，active 不因事务 TTL 过期被删除。
- 管理员必须近期 MFA、TOTP/恢复码防重放、最后管理员保护。
- 邀请只生成普通成员，密码恢复保持 subject 并撤销旧会话。
- Node、Bun、Go、Python SDK 的真实登录/绑定/激活/撤销闭环。

这份清单不代表完整产品/安全验收通过；未完成项仍须实现与验证。

FastNews 真实项目契约：先在同级 FastNews 执行 `uv sync --extra fastcas`，再在本目录执行 `FASTCAS_PROJECT_CONTRACT=1 go test ./internal/httpapi -run '^TestFastNewsAgainstProvider$' -count=1 -v`。使用独立测试数据库；默认 Python 为 FastNews `.venv/bin/python`，可通过 `FASTCAS_NEWS_PYTHON` 覆盖。

签名密钥离线轮换及紧急操作见 [密钥轮换](key-rotation.md)。该操作要求协调停止所有签名实例，保持 encryption.key 不变；接入方缓存处理不能省略。
