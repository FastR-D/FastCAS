# 三语言 SDK 发布设计

日期：2026-09-27
状态：待评审

## 背景

FastCAS 仓库（`FastR-D/FastCAS`）包含 Go 服务端与三个 SDK，但三个 SDK 都只在本地构建，没有发布到任何平台。本设计定义把三个 SDK 发布到对应语言平台的完整方案，并包含 TypeScript 侧从 npm 迁移到 pnpm 的配套改造。

### 已核实的事实

| 事实 | 核实方式 |
| --- | --- |
| 仓库 `FastR-D/FastCAS` 存在且 public | GitHub API 返回 `private: false` |
| `https://github.com/FastR-D/FastCAS` 可直连 | `curl` 返回 200，`git ls-remote` 成功 |
| `fastcas-sdk` 在 PyPI 未被占用 | `pypi.org/pypi/fastcas-sdk/json` 返回 404 |
| `@fastrd/fastcas` 在 npm 未被占用 | `registry.npmjs.org/@fastrd%2Ffastcas` 返回 404 |
| 仓库中**没有任何 LICENSE 文件** | `find -iname 'LICENSE*'` 为空 |
| `sdk/python` 缺 `py.typed` | `find -name py.typed` 为空 |
| `sdk/typescript/dist/` 被 gitignore，仓库无构建产物 | `.gitignore` 与 `git ls-files` |
| 打包含 6 个文件，**不含 LICENSE** | `npm pack --dry-run --json` |
| 根 `go.mod` 已有 `replace ... => ./sdk/go`，`require` 写的是 `v0.1.0` | `go.mod:43` |
| `uv publish` 原生支持 `--trusted-publishing` | `uv publish --help` |
| `pnpm publish` 接受 `--provenance`，dry-run 通过 | `pnpm publish --provenance --dry-run` exit 0 |
| `pnpm pack` 支持 `--pack-destination` 与 `--json` | `pnpm pack --help`、实测 |
| **`pnpm pack --json` 返回对象，npm 返回数组** | 实测：pnpm 给 `{...}`，npm 给 `[{...}]` |
| `pnpm pack --json` 的 `filename` 是绝对路径 | 实测返回 `/tmp/.../fastrd-fastcas-0.1.0.tgz` |
| `pnpm publish` 内部转调 npm | dry-run 输出全部为 `npm notice` |
| `pnpm import` 可从 `package-lock.json` 生成 `pnpm-lock.yaml` | `pnpm import --help` |
| 本地 uv 0.11.19 / pnpm 10.33.0 / corepack 0.34.6 / Node v22.22.2 / Python 3.10.12 | 版本命令 |
| 仓库已有 pnpm 先例：FastRead 前端用锁定的 pnpm 9.15.0 | `docs/implementation-status.md:159` |
| `verify-contracts.sh` 已要求 `corepack` 在 PATH | `scripts/verify-contracts.sh:7` |

### 三个平台的发布机制不同

Go **没有上传接口**。模块代理（proxy.golang.org）是拉取式的：仓库 public 且 tag 名正确时自动抓取，pkg.go.dev 自动建文档页。所以 Go 侧的"发布"就是打一个 tag。

npm 和 PyPI 是主动推送式，需要认证与上传。

## 决策

| 事项 | 决定 |
| --- | --- |
| 许可证 | MIT，三个位置各放一份 |
| 版本策略 | 三个 SDK 锁步在 `0.1.0`，后续版本一起 bump |
| Go 发布范围 | 只打模块 tag，不发 GitHub Release 产物 |
| npm / PyPI 路径 | 两条独立路径，不抽取共享构建模块 |
| 构建入口 | Makefile，npm 与 Python 共用；CI 反过来调 `make` |
| 本地发布 | 允许，用 token 认证 |
| CI 认证 | OIDC trusted publishing；npm 侧保留 token 退路 |
| 触发 | 推 `v0.1.0`，两个平台成功后补推 `sdk/go/v0.1.0` |
| TypeScript 包管理器 | pnpm，覆盖 `sdk/typescript` 与 `web` 两个包 |
| pnpm 版本锁定 | 是，写入 `packageManager` 字段（沿用仓库既有习惯） |
| 交付范围 | 准备到可发布为止，实际发布由用户按按钮 |

## 设计

### 一、LICENSE 放置

MIT 文本需要出现在**三个位置**，少一个就有平台不生效：

| 路径 | 原因 |
| --- | --- |
| `LICENSE` | 仓库根。Go SDK 靠它——tag 指向本仓库，根目录覆盖 |
| `sdk/typescript/LICENSE` | npm **只打包包根目录**的 LICENSE；根目录那份进不去（已实测确认） |
| `sdk/python/LICENSE` | wheel 元数据引用；`pyproject.toml` 的 `license` 字段指向它 |

三份内容必须逐字节相同。

### 二、Python 元数据

`sdk/python/pyproject.toml` 当前只有 `name` / `version` / `description` / `requires-python` / `dependencies`。需补齐：

- `readme = "README.md"`、`license`、`authors`、`urls`（Homepage / Repository / Issues）、`classifiers`
- 放宽依赖：`Authlib==1.8.0` → `Authlib>=1.8,<2`，`httpx==0.28.1` → `httpx>=0.28,<1`。精确钉死会让下游在依赖冲突时无解
- 加 `sdk/python/src/fastcas/py.typed`（PEP 561），并在 `[tool.setuptools.package-data]` 声明，否则不进 wheel

`client.py` 已有完整类型标注，但没有 `py.typed` 时下游 mypy / pyright **完全看不到**，标注等于白写。这是本次改动里对使用者价值最高的一项。

### 三、TypeScript 元数据

`sdk/typescript/package.json` 补齐 `license`、`repository`、`homepage`、`bugs`、`keywords`，并加：

```json
"publishConfig": { "access": "public" }
```

scoped 包（`@fastrd/*`）默认按 restricted 发布——`pnpm publish --dry-run` 的输出确认了这一点（"default access"）。缺这一项直接发不出去。

不加 `prepublishOnly`：CI 与 Makefile 都会显式构建，再加一层会在 CI 里重复构建。

### 四、TypeScript 从 npm 迁移到 pnpm

范围：`sdk/typescript` 与 `web` 两个包，仓库内不再使用 npm 安装依赖。

#### 版本锁定

两个 `package.json` 各加：

```json
"packageManager": "pnpm@10.33.0"
```

corepack 据此解析版本，`verify-contracts.sh` 已经要求 `corepack` 在 PATH，无需新增前置条件。

#### 依赖清单迁移

对每个包：用 `pnpm import` 从现有 `package-lock.json` 生成 `pnpm-lock.yaml`，确认解析结果一致后删除 `package-lock.json`。

**必须删除 `node_modules` 重新安装**。npm 的扁平布局与 pnpm 的符号链接布局不兼容，残留的 npm `node_modules` 配上 pnpm 锁文件会产生难以诊断的构建错误。

#### 需要改动的文件

| 文件 | 改动 |
| --- | --- |
| `sdk/typescript/package.json` | `packageManager`；`test` 脚本内 `npm run build` → `pnpm run build` |
| `web/package.json` | `packageManager` |
| `sdk/typescript/package-lock.json`、`web/package-lock.json` | 删除，替换为各自的 `pnpm-lock.yaml` |
| `Dockerfile` | `COPY web/package.json web/pnpm-lock.yaml ./`；`corepack enable pnpm && pnpm install --frozen-lockfile --ignore-scripts`；`pnpm run build` |
| `.github/workflows/core-sdk.yml` | `cache: pnpm`；`cache-dependency-path` 指向两份 `pnpm-lock.yaml`；`pnpm install --frozen-lockfile`、`pnpm test`、`pnpm run build` |
| `scripts/verify-contracts.sh` | 第 20–21 行改为 `pnpm run build`；第 7 行的必需命令列表 `npm` → `pnpm` |
| `scripts/build-release.py` | 第 57–60、82 行，**且需修 `--json` 解析**，见下 |
| `Makefile` | 新增的发布目标用 pnpm |
| `.env.example`、`docs/development.md`、`web/README.md`、`sdk/typescript/README.md`、`web/src/Integration.tsx` | 命令示例 npm → pnpm |
| `internal/httpapi/{console,fastresearch,fasttask}_browser_test.go` | 构建提示文案 npm → pnpm |

**不改 `docs/implementation-status.md`。** 那是带日期的历史记录，记载的是当时实际执行过的命令。改它等于篡改记录。其中的 npm 引用应保留原样。

#### 关键缺陷：`pnpm pack --json` 的返回结构与 npm 不同

`scripts/build-release.py:59-60` 当前是：

```python
npm_result = json.loads(run("npm", "pack", "--pack-destination", str(stage), "--json", cwd=ts))
npm_package = stage / npm_result[0]["filename"]
```

两处不兼容，实测确认：

| | npm | pnpm |
| --- | --- | --- |
| `--json` 顶层结构 | 数组 `[{...}]` | 对象 `{...}` |
| `filename` 字段 | 文件名 `fastrd-fastcas-0.1.0.tgz` | **绝对路径** `/tmp/.../fastrd-fastcas-0.1.0.tgz` |

`npm_result[0]` 在 pnpm 下对 dict 取下标会抛 `KeyError`，构建直接失败。必须把解析改成兼容两种结构（或按 pnpm 单一形态写死并去掉 npm 分支）。

产物文件名约定相同（`fastrd-fastcas-0.1.0.tgz`），这部分不受影响。

#### 风险：pnpm 10 默认阻止依赖的构建脚本

pnpm 10 起，依赖包的生命周期脚本默认**不执行**，需要 `pnpm approve-builds` 生成白名单。`web` 使用 vite / esbuild，历史上依赖 postinstall 步骤。迁移后必须实测 `web` 能否构建；若失败，在 `package.json` 加：

```json
"pnpm": { "onlyBuiltDependencies": ["esbuild"] }
```

现有配置用了 `npm ci --ignore-scripts`，pnpm 侧对应 `--ignore-scripts` 保留同样的收紧策略。

### 五、Makefile：唯一的构建与发布入口

版本号单一来源是 `sdk/typescript/package.json` 的 `version`（沿用 `build-release.py` 的既有约定）：

```make
VERSION := $(shell python3 -c "import json;print(json.load(open('sdk/typescript/package.json'))['version'])")
```

新增目标：

| 目标 | 行为 |
| --- | --- |
| `check-sdk-version` | 断言 `pyproject.toml` 的 `version` 与 `$(VERSION)` 一致 |
| `publish-dry-run` | 构建两个包 + 隔离冒烟安装，**不碰 registry** |
| `publish-pypi` | `uv build` → 冒烟安装 wheel → `uv publish` |
| `publish-npm` | `pnpm pack` → 冒烟安装 tarball → `pnpm publish <tarball>` |
| `publish` | 先 `publish-pypi`，再 `publish-npm` |

#### 核心不变式：发布的就是冒烟测试装过的那个文件

npm 走 `pnpm pack` 出 tarball、装一遍、再 `pnpm publish <tarball>`，而不是裸 `pnpm publish`。原因有二：`dist/` 在 gitignore 里，仓库没有构建产物，裸 publish 依赖"构建步骤刚好成功"；发布显式 tarball 能保证发出的字节就是验过的字节。

Python 同理：`uv build` 出的 wheel 装进隔离 venv 验证后，再把**同一个文件**交给 `uv publish`。

#### 认证：同一份命令，两种环境

| 环境 | PyPI | npm |
| --- | --- | --- |
| 本地 | `UV_PUBLISH_TOKEN` | `NODE_AUTH_TOKEN`，或先 `pnpm login` |
| CI | OIDC（`--trusted-publishing automatic`） | OIDC；有 `NODE_AUTH_TOKEN` secret 时回退 token |

`uv publish --trusted-publishing automatic`（默认值）的含义正是"在 GitHub Actions 里用 OIDC，否则用 token"，所以本地与 CI 能共用同一条命令。npm 侧不做分支判断，由底层 npm 按 `NODE_AUTH_TOKEN` 是否存在选择。

**注意**：`pnpm publish` 内部转调 npm，所以发布环境里 **npm 二进制仍必须存在**。pnpm 只替代了依赖管理与构建，没有替代发布通道。

#### 发布确认

`make publish` 会**真的往 npm 和 PyPI 推**，而这两个平台的版本号一旦占用不可回收——删了也不能重发。所以 `publish-pypi` / `publish-npm` 在推送前打印目标版本与 registry 并要求输入确认。

CI 无法回答交互提示，用环境变量跳过：

```make
	@if [ -z "$$FASTCAS_PUBLISH_CONFIRM" ]; then \
		printf 'publish %s to PyPI? [y/N] ' "$(VERSION)"; read a; [ "$$a" = y ] || exit 1; fi
```

`make publish-dry-run` 不需要确认，是默认给人试的目标。

### 六、工作流

单个 `.github/workflows/publish.yml`，触发于 tag `v*`，三个 job：

| Job | 依赖 | 内容 |
| --- | --- | --- |
| `pypi` | — | `make publish-pypi` |
| `npm` | — | `make publish-npm` |
| `gotag` | `pypi` + `npm` 都成功 | 补推 `sdk/go/v$(VERSION)` 指向同一 commit |

两个发布 job **互不阻塞、无共享构建状态**，各自独立失败并可单独重跑。选单文件而非两个文件，是因为 `gotag` 需要"两个都成功"才能触发；拆成两个文件后跨 workflow 判断这个条件要靠 `workflow_run` 拼凑，明显更脆。

`permissions`：

```yaml
permissions:
  contents: read
  id-token: write    # trusted publishing (OIDC)
```

`gotag` job 额外需要 `contents: write` 才能推 tag，**只在该 job 上提权**，发布 job 不拿写权限。

用 `environment: pypi` / `environment: npm` 隔离，便于在 GitHub 侧加保护规则。

npm 的 OIDC 依赖 npm CLI 版本（本机 10.9.7 未验证是否满足），因此 workflow 里**显式安装最新 npm** 规避，并保留 token 退路：

```yaml
      - run: npm install -g npm@latest
      - run: make publish-npm
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
          FASTCAS_PUBLISH_CONFIRM: "1"
```

变量名分工：`NODE_AUTH_TOKEN` 是 npm 约定的**环境变量名**（必须叫这个 npm 才认），`NPM_TOKEN` 只是 GitHub secret 的名字，上例通过 `env` 把后者映射到前者。

### 七、首次发布的引导问题

两个平台对"包还不存在"的处理不同，这决定了第一次发布怎么走：

**PyPI 有 pending publisher 机制**——项目尚未创建时就能在账号设置里预登记 trusted publisher（指定 project name、owner、repo、workflow 文件名）。因此 PyPI 的**首次**发布可以直接走 CI + OIDC。

**npm 很可能不支持新包的 OIDC 首发布**——trusted publisher 通常要针对已存在的包配置，而包不存在时无处可配。此点**尚未验证**。

两者共同的稳妥做法：**首次发布用 token**，包创建后再配置 trusted publisher 供后续版本使用：

1. `0.1.0` 首次发布：本地 `make publish` + 你自己的 token（已确认本地可发），或 CI + `NPM_TOKEN` secret
2. 在 PyPI 配置 pending publisher、在 npm 配置 trusted publisher
3. `0.1.1` 起走 CI + OIDC

这是实施的第一步验证项。

### 八、文档更新

| 文件 | 改动 |
| --- | --- |
| `docs/release.md` | 去掉"SDK 未发布"的表述，说明 `make publish-*` 与 tag 流程 |
| `sdk/{go,python,typescript}/README.md` | 加安装命令（`go get` / `pip install fastcas-sdk` / `pnpm add @fastrd/fastcas`），去掉"尚未发布远程 tag" |
| `README.md` | 补 SDK 安装入口 |
| `examples/README.md` | 若引用本地路径安装，改为 registry 安装 |

### 九、与既有 `build-release.py` 的关系

`scripts/build-release.py` 与 Makefile **并存**，职责不同：

- `make release` / `build-release.py`：本地产出完整发布包（服务端二进制、web 包、OpenAPI、三个 SDK），供验收与离线分发
- `make publish-*`：只把 SDK 推到语言平台

`build-release.py` 需要适配 pnpm（第四节表格里的 `--json` 解析修复）。此外它用 `pip wheel --no-build-isolation` 而 Makefile 用 `uv build`，两者造的 wheel 不完全相同。这是"独立路径"决定的**已知取舍**：接受两份构建命令，换取发布流程不被服务端构建阻塞（SDK 发版不应因 CGO 编译或 React 构建失败而卡住）。

`build-release.py` 现有的"TS 版本 == Python 版本"校验保留，并扩展为同时提示 Go tag 名。

## 不做的事

- 不发 GitHub Release 二进制产物（Go 只打 tag）
- 不实现服务端 Docker 镜像发布
- 不建 pnpm workspace（两个包各自独立锁文件，与现状结构一致）
- 不引入 changesets 等版本管理工具（三个包锁步，两个文件手动 bump 足够）
- 不改 SDK 的运行时代码逻辑
- 不改 `docs/implementation-status.md`（历史记录）

## 风险

| 风险 | 缓解 |
| --- | --- |
| 版本号占用不可回收 | 发布前确认提示；`publish-dry-run` 为默认路径；首次用 `0.1.0` 而非 `1.0.0` 保留语义弹性 |
| pnpm 10 阻止依赖构建脚本，vite/esbuild 可能构建失败 | 迁移后立即实测 `web` 构建；必要时配 `pnpm.onlyBuiltDependencies` |
| 残留 npm `node_modules` 与 pnpm 锁文件混用 | 迁移脚本显式删除 `node_modules` 后重装 |
| `build-release.py` 的 `--json` 解析在新版工具下失效 | 本设计已定位该缺陷；改为兼容两种返回结构 |
| npm OIDC 版本门槛未知 | workflow 显式装最新 npm；保留 token 退路 |
| npm 新包无法 OIDC 首发布 | 首次走 token，见第七节 |
| `@fastrd` scope 权限 | 已确认是用户自己的 org；CI 首次运行会立即暴露问题 |
| Docker 构建需在 alpine 内启用 corepack | `corepack enable pnpm` 后再 `pnpm install`；Node 22 自带 corepack |

## 验证方式

实施完成后需能跑通：

1. `make publish-dry-run` — 两个包构建成功，隔离安装通过（本地无 token 也能跑）
2. 解包 `uv build` 产出的 wheel，确认内含 `py.typed`
3. `pnpm pack --dry-run --json` 的文件列表中**包含** `LICENSE`
4. `uv publish --dry-run` 不报元数据错误
5. 三份 LICENSE 内容逐字节相同（`sha256sum` 比对）
6. pnpm 迁移后 `pnpm run build` 在 `sdk/typescript` 与 `web` 均成功；`web` 的 vite 构建确认未被构建脚本策略挡住
7. `scripts/build-release.py` 在 pnpm 下跑通，`make release` 产出完整
8. 打 tag 后 `GOPROXY=https://proxy.golang.org go list -m github.com/FastR-D/FastCAS/sdk/go@v0.1.0` 成功；并在一个删掉 `replace` 的临时消费项目里 `go get` 一次，确认不依赖本地路径
9. workflow 文件通过 `actionlint` 或 GitHub 的语法校验

## 待验证项

1. npm trusted publishing 要求的最低 npm CLI 版本
2. npm 是否允许新包首次发布走 OIDC
3. pnpm 10 的构建脚本策略是否影响 `web` 的 vite 构建
4. `pnpm install --frozen-lockfile` 在 alpine 容器内的行为（Dockerfile 路径）
