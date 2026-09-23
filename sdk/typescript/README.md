# @fastrd/fastcas

可运行的 Node 服务身份调用见 [三语言示例](../../examples/service-tokens/README.md)。

开发中的 FastCAS TypeScript SDK。`npm run build` 生成 ESM 与类型声明；`./browser` 不包含服务端凭证代码，`./server` 面向 Node/Bun。

应用必须提供持久化 `TransactionStore`，其中 `take(state)` 原子消费事务。为每次流程设置随机、HttpOnly、生产 Secure 的浏览器绑定 Cookie，并在回调传入相同绑定；账号认证前由应用重新验证本地账号，回调检查本地 account/session 未改变。

`beginLogin → finishLogin` 返回已验证身份，不自动新建/合并本地账号。已有账号认证使用 `beginLink → finishLogin → prepareLink`，应用提交 pending 关系后调用 `activateLink`，再提交 active 关系。登录时使用 `resolveLink` 映射回原本地 ID，并检查本地账号状态与权限。

显式新用户注册使用 `beginRegistration({newLocalAccountRef, browserBinding})`，事务 purpose 为 `register`。应用先检查自己的注册政策并预留全新随机账号 ID；回调验证成功、prepare 后，在同一事务内拒绝任何已存在的该 ID，创建普通用户与 pending 绑定，再 activate。不能将此接口用于绑定旧账号；旧账号必须走要求本地证明的 `beginLink`。同邮箱不代表同账号。

关闭集成时不要构造全站鉴权门禁。本地密码、角色、工作区及原会话继续由项目管理。CAS 上游 refresh token 只存服务端，应用要序列化刷新操作。资源服务先用 `verifyAccessToken` 检查签名、受众、类型及 scope；需要即时中心撤销状态时再用保密资源客户端的 `introspectToken`。API Access Token 的离线验签不证明即时撤销状态。

当前 Node/Bun 真服务契约见 `test/contract.mjs`，已覆盖注册事务、登录、绑定与撤销；`handleNotification` 支持签名绑定撤销与身份状态通知，旧 `handleEvent` 仍仅接受绑定撤销。设备授权、受限 token exchange SDK 方法已有真实服务契约；框架适配器和完整发布清单仍在开发。

## 签名公钥缓存与紧急撤销

常规验证缓存公钥最多五分钟，未知 kid 刷新有三十秒冷却，短期请求可能需要重试。紧急撤销应先停止接收并排空认证请求，再由受信运维调用 `sdk.resetVerificationCache()`，然后恢复请求；同时处理全部进程与实例。方法清理 discovery/JWKS，本次后续验证必须重新取得公钥，网络失败不会放行。不要暴露为未认证 HTTP 接口。已建立的项目会话需要独立撤销。
