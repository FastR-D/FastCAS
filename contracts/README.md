# 跨语言共享契约

TypeScript、Go、Python 的独立 SDK 测试读取同一个 [访问令牌 fixture](access-tokens.json)，覆盖用户、服务、委托三个正例，以及 issuer、audience、签名、算法、未知 key、用途、过期、未来时间、缺失字段和 scope 的反例。只使用离线 HTTP transport，不访问真实身份服务。

fixture 与 Go 共享契约测试位于本目录，主模块 `go test ./contracts` 执行；TS/Python 直接读取同一个文件，没有另存副本。Go SDK 模块没有仓库外 fixture 依赖，`sdk/go/testdata/` 为被忽略的本地目录。`node contracts/generate.mjs` 可重新生成。生成器只在内存中使用临时私钥，文件内仅含公钥和合成令牌。固定时间覆盖 2023–2100 是避免 fixture 自然过期，不是生产令牌期限；生产访问令牌仍为五分钟。

共享 fixture 不代替真实 PostgreSQL/提供方契约。`make verify-contracts` 还验证三运行时登录、PKCE/nonce/state、单次回调、prepare/activate、撤销、事件与七项目账号及资源保持。
