# 签名密钥离线轮换

`rotate-keys` 为已实现的离线运维入口，不是在线管理控制台。签名使用 RSA 3072 / RS256；轮换保持 `encryption.key` 不变，因此不会因本操作破坏已有会话加密或 MFA 密文。不要用重新执行 init-keys 代替轮换。

## 常规轮换

1. 停止所有使用该密钥目录的 FastCAS 实例，记录停止时间，确认没有旧实例继续签发。
2. 对受控密钥目录做加密备份；备份包含私钥，仅由运维保管。
3. 执行 `go run ./cmd/fastcas rotate-keys --keys .local/keys --retain 24h`。
4. 将完整目录安全分发到全部实例后重新启动；验证 JWKS 同时发布新 kid 与保留期内的旧 kid，新令牌使用新 kid，旧令牌仍可验证。
5. 确认各接入方可在未知 kid 时刷新 JWKS。保留期结束后，服务不再发布该旧公钥。

当前 ID/access/event 令牌有效期为五分钟。命令拒绝小于十分钟的保留期；默认 24 小时。必须结合接入方缓存策略选择更长时间。计时从轮换写入时开始，停机不得耗尽所需旧令牌验证窗口。

首次轮换从既有 signing.pem 读取，随后以 0600 的 keyring.json 为权威。配置包含新私钥及有期限的旧公钥；临时文件写入并 fsync 后原子重命名、同步目录，再删除旧 signing.pem 并再次同步目录。连续轮换保留尚未到期的旧公钥，不保留旧私钥。不得删除 keyring.json 来回退；损坏或权限过宽的 manifest 会拒绝启动。

rotation.lock 排斥并发轮换；崩溃后的遗留锁必须在确认没有活跃轮换进程后由运维处理。它不是服务实例停机锁，命令无法证明所有实例已停止。

## 私钥泄露的紧急处理

停止全部旧签名实例，执行 `go run ./cmd/fastcas rotate-keys --keys .local/keys --emergency`，重新分发并启动。紧急模式不保留任何旧验证公钥，包括更早轮换保留的键。

仅删除 JWKS 公钥不能立即使各 SDK 已缓存的公钥失效。应同步重启/清理所有接入方 JWKS 缓存，并按事故范围撤销既有凭证和会话；隔离旧密钥备份，禁止恢复旧 signing.pem。恢复旧数据库快照时可先运行 `fence-restore` 撤销快照凭证与绑定。自动传播紧急撤销和在线轮换控制台尚未完成，不能将该命令视为完整泄露响应。

## 验证范围

`go test ./internal/core -run 'TestOfflineSigningRotation|TestRotationRefuses' -count=1` 验证旧签名仍可由保留键验证、新旧键不同、加密材料不变、连续轮换、到期停止发布、紧急移除、权限校验、并发锁和损坏 manifest 拒绝回退。真实 HTTP / PostgreSQL 联调 `go test -race ./internal/httpapi -run '^TestRealJWKSOfflineRotationAndVerifierCache$' -count=1 -v` 已通过：签发实际 ID Token、替换服务处理器模拟停机重载、JWKS 只含公钥、Go SDK 使用的 coreos 验证库未知 kid 刷新、旧令牌保留、紧急移除后新验证器拒绝旧键、网络不可用时新验证器拒绝令牌。测试明确断言已有缓存仍接受旧键，避免将 JWKS 移除误称为即时撤销。TypeScript/Python 缓存行为和多进程部署演练尚待验证。

TS/Python SDK 缓存契约已通过本地签名与模拟 HTTP 测试，覆盖 30 秒冷却、未知 kid 更新、已缓存旧键、清理后拒绝旧键、断网拒绝。TS `resetVerificationCache()`、Python `reset_verification_cache()` 与 Go `ResetVerificationCache()` 均须在可信运维流程暂停并排空认证请求后对每个实例调用；TS 同时清理 openid-client 配置。Go SDK 已用真实 Go/PostgreSQL 提供方令牌验证未知 kid 刷新、缓存旧键、紧急清理及断网拒绝；`Diagnose(ctx)` 只输出脱敏配置。不能在并发认证中把清理方法视作全局撤销屏障。三语言跨进程部署演练仍待完成。
