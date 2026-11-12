# FastResearch 角色与个人动态接入

`profile` scope 的 UserInfo 返回当前中心身份 `role`，Research 据此继承管理员身份。此字段在 UserInfo 请求时读取当前身份；Research 管理操作每次复核，不能用本地 Key 提权。没有 profile scope 不额外披露角色。

Read、News、Write 的个人动态统一使用服务身份 `fastresearch`、scope `workspace:read`。由管理员在应用权限页为该服务身份增加 scope，并将接收应用实际 client ID 加入 resources（例如 `fastread`、`news`、`fastwrite`）。保留原有 scope/resources，仍仅允许 client_credentials；不改变交互客户端 research。

管理员 API：`POST /api/v1/admin/applications/fastresearch/capabilities`，需要近期 MFA、同源和 CSRF，请提交原有 `grant_types`、`scopes`、`resources` 与新增权限的并集。此版本不会自动修改已有生产授权或密钥。接收应用要调用 introspection，因此资源值应使用其实际 client ID。

三个子应用的只读端点为 `POST /api/integrations/fastresearch/workspace`，subject 由 Research 已认证会话确定。子应用验证签名、服务标识、scope、aud、中心实时状态及双端账号绑定。完整契约见 FastResearch 的 `docs/integrations/workspace-activity.md`。
