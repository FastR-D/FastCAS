# SDK 发布与本地产物

三语言 SDK 锁步发布。版本源为 `sdk/typescript/package.json`，同时修改 `sdk/python/pyproject.toml`；`make check-sdk-version` 会检查两者一致，CI 还检查触发 tag 必须等于 `v<版本>`。目前发布入口仅接受稳定版本 `X.Y.Z`。

## SDK 持续集成

`.github/workflows/sdk.yml` 在分支 push、pull request 和手动触发时分别运行 Go、Python、TypeScript job，无需数据库。Go 执行 race 测试和 vet；Python 在 3.10/3.13 测试并验证 wheel 安装；TypeScript 测试并验证 tarball 安装。发布工作流复用该 CI，全部通过后才允许上传。原有 core-sdk 工作流保留服务端协议集成验证。

## 本地 SDK 发布

需要 Python 3.10+、uv、Node 22.22.2、npm 和 pnpm 10.33.0（可运行 `corepack enable pnpm`）。pnpm 管理依赖与构建，发布时仍需 npm 二进制。

```sh
make check-sdk-version
make publish-dry-run
# 配置 UV_PUBLISH_TOKEN（PyPI token）和 NODE_AUTH_TOKEN（npm token）后：
make publish
# 或单独执行：
make publish-pypi
make publish-npm
```

`publish-dry-run` 构建 Python wheel 和 npm tarball，在临时隔离环境安装、检查导出，并检查 LICENSE 和 `py.typed`，不会调用上传接口；安装构建依赖仍需网络。临时产物在结束时清理。实际发布上传的正是该次冒烟安装验证过的文件，Python 仅发布 wheel。每次上传前显示版本和目标 registry，输入 `y` 才继续；`FASTCAS_PUBLISH_CONFIRM=1` 用于 CI 跳过提示。`make publish` 串行先 PyPI 后 npm，即使使用 `make -j` 也保持顺序。

npm 也可以先 `pnpm login`，用 `pnpm whoami` 检查账号；本地 2FA 发布可执行 `env npm_config_otp=当前验证码 make publish-npm`（当前固定 pnpm 10）。存在 `NODE_AUTH_TOKEN` 时脚本自动生成仅含环境变量引用的临时 `.npmrc`。PyPI 使用 `UV_PUBLISH_TOKEN`；不要把 token 写入仓库或命令历史。发布目标固定为正式 PyPI 和 npm。任一平台失败后，查清已成功的平台，只重试失败的 `publish-*` 目标；不要重复上传已占用的版本。

## GitHub Actions 发布

`.github/workflows/publish.yml` 由推送 `v*` tag 触发：SDK CI 通过后，`pypi` 和 `npm` 两个 job 独立构建、验证、上传；全部成功后 `gotag` 创建指向相同提交的 `sdk/go/v<版本>`。已存在的 Go tag 必须指向相同提交，否则失败，绝不覆盖。只有 `gotag` 获得仓库写权限；发布 job 获得 OIDC 权限。

首次运行前在平台网页配置：

1. GitHub 仓库 `FastR-D/FastCAS` 创建 `pypi`、`npm` environments，可设置审批人及允许的 release tags。
2. PyPI 在账号的 Publishing 设置添加 pending publisher：项目 `fastcas-sdk`、owner `FastR-D`、repository `FastCAS`、workflow `publish.yml`、environment `pypi`。项目已存在时在项目 Publishing 设置配置同样内容。
3. npm 首次发布使用有 `@fastrd` 发布权限的 token，存为 GitHub `npm` environment 的 `NPM_TOKEN` secret，或先本地运行 `make publish-npm`。包存在后在 npm 包 Settings → Trusted publishing 配置 GitHub Actions、owner `FastR-D`、repository `FastCAS`、workflow `publish.yml`、environment `npm`，然后移除 token secret，使用 OIDC。CI 通过 pnpm 安装 npm CLI；官方要求 npm >=11.5.1、Node >=22.14.0。
4. 提交所有变更，通过测试后，创建并推送 `v0.1.1`（后续替换为实际版本）。不要在同版本本地发布后又触发全量 CI 上传；npm 0.1.0 的本地发布与本次 0.1.1 发布独立；本次通过新版本 tag 统一发布。

```sh
git tag v0.1.1
git push origin v0.1.1
```

部分 job 失败时使用 GitHub 的 **Re-run failed jobs**，避免重新运行成功上传的 job。若选择全部本地发布，两个平台都成功后手动将 `sdk/go/v0.1.1` tag 指向本次发布提交并推送，不必推送会再次上传的 `v0.1.1` tag。

发布后的 Go 消费验证须在仓库外的临时目录执行，避免本地 `replace` 掩盖远程问题：

```sh
mkdir /tmp/fastcas-consumer && cd /tmp/fastcas-consumer
go mod init example.com/fastcas-consumer
GOPROXY=https://proxy.golang.org go get github.com/FastR-D/FastCAS/sdk/go@v0.1.1
```

平台配置、真实上传和远程 Go tag 验证需在首次发布时完成；本地 dry-run 不证明这些步骤已经成功。参考 [npm trusted publishing](https://docs.npmjs.com/trusted-publishers/) 与 [PyPI pending publishers](https://docs.pypi.org/trusted-publishers/creating-a-project-through-oidc/)。

## 完整本地离线产物

通过 `make verify-contracts` 后，安装两个前端目录的依赖（`pnpm install --frozen-lockfile --ignore-scripts`），并为 Python 配置 setuptools/wheel，然后运行 `make release`。它独立构建 Linux/amd64 服务端、控制台、TypeScript tarball、Python wheel、Go SDK 源码快照和 OpenAPI，隔离安装或编译三个 SDK，输出到 `dist/release-v<版本>/`。非空输出目录会被拒绝，可用 `python3 scripts/build-release.py --output /path/to/empty-directory` 指定其他目录。

`manifest.json` 记录版本、Go tag 与产物，`SHA256SUMS` 可用 `sha256sum -c SHA256SUMS` 校验。离线构建不上传 registry；Go 源码归档与远程模块 tag 是两个独立入口。核心 CI 在协议/SDK 测试后运行该入口并保存产物，同时运行 `make publish-dry-run` 检查正式发布构建路径。

升级与回滚参考[容器恢复演练](compose-recovery.md)、[单主机 HTTPS 样例](production-compose.md)和[全项目门禁](verification.md)。真实部署前仍需在目标环境验收。
