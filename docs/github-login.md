# GitHub 登录、注册与已有账号绑定

GitHub 用户认证使用 OAuth 2.0 Authorization Code + PKCE（S256）；FastCAS 继续通过 OIDC 向下游应用提供统一登录，不改变已有客户端配置。GitHub Actions 的 OIDC issuer 不用于用户登录。

## 配置

在 GitHub 创建 OAuth App，Homepage URL 填 FastCAS 的公开 origin，Authorization callback URL 固定填写：

```text
https://cas.fastrd.io/auth/github/callback
```

开发实例使用自己的回环地址，例如 `http://127.0.0.1:8900/auth/github/callback`。开发和线上建议分别创建 OAuth App。回调地址由 `FASTCAS_ISSUER` 推导，不读取浏览器提供的地址或代理头。

| 环境变量 | 作用 |
| --- | --- |
| `FASTCAS_GITHUB_CLIENT_ID` | OAuth App 的 Client ID |
| `FASTCAS_GITHUB_CLIENT_SECRET` | OAuth App 的 Client Secret，仅注入服务端 |
| `FASTCAS_GITHUB_REGISTRATION_ORG` | 新用户注册时必须属于的 GitHub 组织；线上配置为 `FastR-D` |

Client ID 和 Secret 都未配置时，GitHub 入口关闭；只配置其中一项会拒绝启动。只有 `FASTCAS_ENV=development` 时组织变量为空才允许任意 GitHub 用户注册；其他环境组织变量为空时自动使用 `FastR-D`。注册限制仅作用于尚未绑定的 GitHub 身份创建新 FastCAS 账号，已有绑定的登录、已有 FastCAS 账号的显式绑定不受此限制。线上（非 development）GitHub 新注册账号默认角色为 `admin`，开发环境默认为 `member`，无需新增角色环境变量。已有账号登录或绑定 GitHub 保留原角色，不自动提权；邀请注册继续使用管理员签发的一次性凭证，默认角色仍为普通成员。

上线前先运行 `fastcas migrate`，应用新增的 `018_github_login.sql`，再重启服务。生产 FastDeploy workflow 已配置组织限制，并从仓库 Secrets 读取 `FASTCAS_GITHUB_CLIENT_ID`、`FASTCAS_GITHUB_CLIENT_SECRET`。需要维护者在 GitHub 仓库中设置这两个 Secret；不会把真实凭证提交到代码中。Compose 样例也传递这两个变量。

注册申请 `user:email`，启用组织限制时额外申请 `read:org`。服务端使用用户访问令牌读取 `/user/memberships/orgs/FastR-D`，仅接受 `state=active`，支持非公开成员。待接受邀请、非成员、组织未批准 OAuth App、SAML 未授权、API 限流或不可用均不会创建账号。若 FastR-D 开启 OAuth App 访问限制，需组织管理员批准该 App；使用 SAML 时用户还需完成组织要求的授权。

GitHub 官方说明：[OAuth 授权与 PKCE](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps)、[组织成员 API](https://docs.github.com/en/rest/orgs/members#get-an-organization-membership-for-the-authenticated-user)、[OAuth App 的组织授权](https://docs.github.com/en/apps/oauth-apps/using-oauth-apps/authorizing-oauth-apps)。

## 使用

- 登录页的“使用 GitHub 注册”在线上创建管理员账号，开发环境创建普通成员账号，使用 GitHub 已验证的主邮箱及姓名；新账号不预设本地密码，可在登录后五分钟内从安全中心设置 FastCAS 密码。
- 已有账号先用原方式登录，在安全中心重新验证身份，再点击“绑定 GitHub”。启用 FastCAS 双重验证时也需要近期 MFA；授权返回时必须仍是发起绑定的同一个有效会话。
- “使用 GitHub 登录”只登录已绑定的账号，不自动注册。注册入口遇到已绑定 GitHub 时按已有账号登录处理。
- 同邮箱不自动合并。邮箱已存在时，新用户注册会提示先登录已有 FastCAS 账号再绑定。一个 GitHub 固定用户 ID 只能绑定一个 FastCAS 账号，每个 FastCAS 账号只能绑定一个 GitHub。
- 已启用 FastCAS MFA 的账号使用 GitHub 登录后仍需输入 FastCAS 动态验证码或恢复码。新注册的管理员需先在安全中心启用 MFA，管理接口仍要求近期 MFA。禁用或删除账号不能通过 GitHub 登录。
- 从下游 OIDC 授权或设备授权进入时，GitHub 登录完成后返回原流程；下游应用授权仍需用户确认。

授权状态保存在 PostgreSQL 中，十分钟过期，绑定独立的 HttpOnly 浏览器 Cookie，并通过原子删除防止跨实例重放；绑定发起和 MFA 提交均校验 Origin 与 CSRF。GitHub access token 仅在回调请求中短暂使用，不写入数据库、Cookie、API 响应或审计记录。删除账号会移除 GitHub 绑定；恢复快照隔离命令 `fence-restore` 会清除 GitHub 绑定和未完成流程，需要用户用恢复后的本地认证方式重新绑定。请为仅使用 GitHub 的账号先准备密码或管理员密码恢复流程。

## 验证

使用独立 PostgreSQL 测试库运行：

```bash
FASTCAS_TEST_DATABASE_URL='postgres://…' go test -race ./...
go vet ./...
python3 api/check.py
pnpm --dir web run build
FASTCAS_TEST_DATABASE_URL='postgres://…' FASTCAS_BROWSER_CONTRACT=1 \
  go test -race -run '^Test(GitHubAgainstRealBrowser|ConsoleAgainstRealBrowser)$' ./internal/httpapi
```

GitHub 专项测试使用模拟的上游响应，验证正式成员、待接受邀请、组织拒绝/不可用、邮箱未验证/冲突、账号绑定及会话变化、PKCE、状态过期/重放、MFA、账号禁用、OIDC/设备返回、并发注册、本地密码设置，以及线上默认管理员、开发默认成员、已有绑定不提权和管理员 MFA 门槛。浏览器测试覆盖注册、绑定及密码设置界面；真实 OAuth App 与组织策略仍需在凭证配置后联调。
