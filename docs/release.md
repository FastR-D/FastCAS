# 本地发布产物与边界

在通过 `make verify-contracts` 后，从 FastCAS 目录执行 `make release`。入口检查 OpenAPI 与实际路由，构建服务 Linux/amd64 二进制和控制台，打包 TypeScript SDK、Python wheel、Go SDK 源码快照及 OpenAPI，并在隔离目录安装或编译三种 SDK。输出写入 `dist/release-v0.1.0/`；已有非空目录会被拒绝，避免覆盖先前验收的产物。可用 `python3 scripts/build-release.py --output /path/to/empty-directory` 指定其他位置。

产物内的 `manifest.json` 列出版本和文件，`SHA256SUMS` 可在目录内用 `sha256sum -c SHA256SUMS` 校验。脚本只收集显式指定的构建产物和源码，不打包 `.env`、`.local`、私钥、数据库或测试凭证。使用固定的归档元数据和 Python wheel 时间戳；本机连续两次独立构建的全部文件及校验和逐字节相同。CI 在核心协议/SDK 测试通过后运行同一入口并上传本地构建产物。

Go SDK tarball 是可核验的源码快照，**不是** Go module proxy 发布。正式发布仍需将 FastCAS 放入实际 Git 仓库，确定 SDK 子模块 tag `sdk/go/v0.1.0`，登记并发布 npm/Python 包，验证安装者从远程 registry 获取的版本和本地清单一致。当前 CI 文件只是工作区内的配置，未证明托管 CI、生产镜像、真实域名、七项目部署迁移或生产数据已完成。

升级与回滚应使用[容器恢复演练](compose-recovery.md)、[单主机 HTTPS 样例](production-compose.md)和[全项目门禁](verification.md)；任何真实部署前须用目标实例的域名、数据库与项目回调地址重新验收。FastCAS 始终可选，各项目的本地账号、登录与注册流程不应依赖本发布包才能运行。
