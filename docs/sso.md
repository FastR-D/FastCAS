# 无感登录

已注册项目使用授权码和 S256 PKCE 登录时，CAS 优先复用有效的中心浏览器会话，直接回调项目；没有会话时显示登录，完成密码或 GitHub 登录（以及已开启的 MFA）后直接回调，不再额外点击“确认并继续”。项目无需修改 SDK 或降低 token 校验。

常规身份范围 `openid profile email offline_access` 使用管理员注册应用时授予的信任，不逐次弹出确认。资源权限首次确认后按身份和应用保存，后续相同或更少权限的登录直接通过；新请求的资源权限仍需确认。账号绑定以及显式 `prompt=consent` / `prompt=select_account` 仍显示确认。设备授权保留双端确认。

`prompt=none` 始终不显示登录或确认页面：有效会话直接签发一次性授权码；无有效会话返回 `login_required`；需要绑定或范围确认时返回 `interaction_required`。错误通过已验证的回调地址返回并保留 state。项目应处理此结果，必要时由用户发起正常登录。

`prompt=login` 和 `max_age=0` 要求本次事务内重新认证；正数 `max_age` 按最近认证时间判断。过期、撤销或禁用账号的会话不可复用。授权完成时再次在数据库事务中检查会话和账号状态，授权码仍只能兑换一次。

协议依据：[OpenID Connect Authentication Request](https://openid.net/specs/openid-connect-core-1_0.html#AuthRequest)。专项回归在专用 PostgreSQL 库运行：

```bash
pnpm --dir web run build
FASTCAS_BROWSER_CONTRACT=1 go test -race ./internal/httpapi -run 'TestSSO|TestFreshPassword|TestGitHubSSO' -count=1 -v
```

浏览器测试运行两个独立接入方及真实 Go SDK，覆盖首次登录、跨项目复用、静默成功与失败、强制登录和显式确认，分别验证启用及禁用 JavaScript 的导航。
