# fastcas-sdk

可运行的服务身份调用见 [三语言示例](../../examples/service-tokens/README.md)；它按资源受众验签并查询中心撤销状态，进程不会输出令牌。

开发安装：`uv pip install -e sdk/python`。支持 Python 3.10+。`FastCAS` 提供同步 API；`AsyncFastCAS` 用工作线程适配异步调用，不阻塞事件循环，HTTP 超时限制后台调用时间。

生产必须提供持久 `TransactionStore`；`SQLiteTransactionStore(path)` 适用于单机多进程，数据库权限应为 0600。多主机部署使用共享事务数据库适配器。回调原子消费 state，并校验浏览器绑定、PKCE、nonce、签名、issuer、audience 与 userinfo subject。

已有账号认证使用 `begin_link/finish_login/prepare_link/activate_link`。应用在 prepare 与 activate 之间提交本地 pending 映射，之后持久化 active。SDK 不按邮箱合并，不改变本地 ID、权限和密码。

显式新用户注册使用 `begin_registration(browser_binding, new_local_account_ref=...)`，需要应用检查注册政策、预留全新 ID，并在验证回调后于本地事务内拒绝已存在的 ID，创建普通用户和 pending 关系再 activate。此流程不能用于绑定原账号，原账号继续使用有本地证明的 `begin_link`。

`handle_notification(token, apply)` 验证签名绑定撤销与身份状态事件；旧 `handle_event` 仍仅接受绑定撤销。回调在本地事务内完成事件 ID 去重、绑定版本比较及状态更新，停用只影响 FastCAS 来源会话。`AsyncFastCAS.handle_notification` 接收异步回调并等待其提交，失败向上抛出。重复已提交事件返回成功，事务失败由 HTTP handler 返回非 2xx。详见 [事件契约](../../docs/events.md)。

资源服务先调用 `verify_access_token(token, audience, scopes)` 做离线签名和权限验证，再调用 `introspect_token(token)` 查询中心撤销状态；异步 facade 可 `await introspect_token(token)`。资源客户端必须有自己的保密凭证和受众权限。真实提供方契约已验证 token 撤销后返回 `active=false`。仅离线验签不等于即时撤销。

当前 Python 真服务契约、SQLite 并发消费、签名事件及设备授权/轮询测试已实现；原生异步 HTTP adapter 与完整发布仍在开发。

FastAPI 可选适配层位于 `fastcas.fastapi`，导入主包不加载 FastAPI。现有项目路由可 `await receive_event(request, sdk, apply_event)` 或 `await receive_logout(request, sdk, apply_logout)`；支持同步 `FastCAS` 与异步 `AsyncFastCAS`。它们限制通知体为 64 KiB，验证签名，并等待项目回调提交；回调须在本地数据库事务中完成 ID 去重、绑定版本处理以及只撤销 FastCAS 来源会话。验签失败返回 401，本地提交失败返回 503 供 outbox 重试。FastRead 已接入适配层，项目侧仍保留本地登录和工作区授权。

## 签名公钥缓存与紧急撤销

常规验证缓存公钥最多五分钟，未知 kid 刷新有三十秒冷却，短期请求可能需要重试。紧急撤销应先停止接收并排空认证请求，再由受信运维调用 `sdk.reset_verification_cache()（异步 facade 使用 await）`，然后恢复请求；同时处理全部进程与实例。方法清理 discovery/JWKS，本次后续验证必须重新取得公钥，网络失败不会放行。不要暴露为未认证 HTTP 接口。已建立的项目会话需要独立撤销。
## 公开设备客户端

使用 `Configuration(issuer, client_id, issuer + "/device")`（无 client secret）建立 SDK，调用 `device_authorize(["openid", "profile"])`，向用户显示返回的 `verification_uri` 和 `user_code`。每次按 `interval` 调用 `poll_device(device_code)`；`authorization_pending` 继续等待，`slow_down` 后增加间隔。成功后 SDK 验证 ID Token 与访问令牌的签名、issuer、audience 和 subject 一致。客户端仍需本机操作人确认，不能把设备令牌直接当作本机命令权限。FastLabs 示例见 [接入记录](../../../FastLabs/README.md)。
