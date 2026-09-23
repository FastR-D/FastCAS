# 设备授权与 FastLabs 本机关联

FastCAS 提供 RFC 8628 设备授权入口 `/oauth/device_authorization`，发现文档公开该地址。客户端需登记为公开客户端，grant 为 `urn:ietf:params:oauth:grant-type:device_code`，scope 至少 `openid`。设备码有效 10 分钟，初始轮询间隔 5 秒；过快轮询返回 `slow_down` 并增加间隔。设备码只以摘要落 PostgreSQL，用户代码有唯一约束、过期清理和浏览器侧限速。已批准的设备码在取令牌时原子消费，重放拒绝。此流程发短期访问令牌和 ID Token，不发刷新令牌。

用户在 `/device` 核对应用、权限和设备代码后明确允许或拒绝。确认要求有效 FastCAS 浏览器会话、同源 Origin 和绑定该用户代码的表单 CSRF。若尚未登录，登录后会回到对应设备确认页。登录 FastCAS 不自动确认设备。

FastLabs 将 FastCAS issuer 与公开客户端 ID 配在 `.fastlab/fastlab.env`，需要时才导入 Python SDK。本机“设置 → FastCAS 关联”发起设备授权、按间隔轮询，在本机再次确认已验证的 subject 后，以短期设备访问令牌和稳定 installation ID 在中心登记。访问令牌只驻内存；中心返回的一次性安装管理密钥保存在本机 0600 文件，只能查询或撤销该安装，不作为本机任务或飞书命令权限。解除映射不删除任务。所有本机网页写接口均检查环回 Host、同源 Origin 与 CSRF。

FastCAS 安全中心“本机安装”按每页 50 条列出用户的安装记录，支持继续加载旧安装和近期重新认证后的撤销。`GET /api/v1/me/device-installations` 返回 `installations` 与 `next_cursor`，下一页将游标作为 `before` 参数；游标只对所属用户有效。撤销安装与其设备访问令牌在同一数据库事务内完成，在线 introspection、userinfo 和中心 API 随即拒绝旧令牌。FastLabs 打开本机设置状态时最多每 15 秒联网检查一次并同步中心撤销，断网不阻断本机任务。本机离线解除关联会先清除本地映射，保留待撤销凭据至联网重试。设备授权没有跨项目 token exchange 授权，浏览器无法用此配对远程启动本机命令。独立离线 JWT 验签者仍可能接受令牌到其最多 5 分钟到期；需要即时撤销的资源端须查询中心状态。

升级前仅保存在本机的旧关联继续保留，不会凭空出现在中心清单。用户可在本机解除旧关联并重新配对，使安装进入中心管理。

恢复旧数据库快照后应先运行 `fence-restore`：快照内的安装记录会被撤销；快照之后才配对的安装因中心不再认识其凭据，本机下次联网检查会清除关联并允许重新配对。网络不可达时本机任务继续运行，不据此删除关联。

联调：`FASTCAS_PROJECT_CONTRACT=1 go test ./internal/httpapi -run 'TestDeviceAuthorizationSingleUseAndBrowserConfirmation|TestFastLabsAgainstProvider' -count=1 -v`。真实 Chrome/FastLabs 本机页/FastCAS/PostgreSQL 浏览器契约 `FASTCAS_BROWSER_CONTRACT=1 go test -race ./internal/httpapi -run '^TestFastLabsAgainstRealBrowser$' -count=1 -v` 覆盖设备确认页登录、用户批准、本机二次确认、稳定安装 ID、超过一页的中心清单、撤销、本机状态收敛及任务列表保持；本机主动解除另由真实提供方联调和本地测试覆盖。此前测试发现设置页定时刷新反复替换操作按钮，现数据未变化时保留原节点。FastLabs 回归：`PYTHONPATH=.:tests python3 -m unittest discover -s tests -v`。
