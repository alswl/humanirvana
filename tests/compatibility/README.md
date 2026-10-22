# TDD migration application demo

这是用于提前验证迁移方案的合成订单应用。原 Nirvana 与 Huma 兼容版本使用同一个 `operations` 定义工厂、原业务方法、validator 和中间件；两边分别绑定隔离的内存仓库，不重写 Huma 业务 handler。

## 运行

需要 Go 1.23+。在仓库根目录运行：

```sh
make demo-tdd
make demo-app
```

若本机 Go 由 asdf 管理但尚未选择版本，可指定已安装的 Go，例如 `make demo-tdd GOCMD=/opt/homebrew/opt/go@1.23/bin/go`，启动 Demo 时同样传入 `GOCMD`。

`demo-tdd` 在进程内运行全部测试，自动管理服务生命周期，不依赖固定端口、不跳过未启动的后端、不终止其他进程。`demo-app` 默认监听 `127.0.0.1:18080`。

```sh
# 直接请求原后端和迁移后端；两边初始数据相同，写入互相隔离。
curl -i http://127.0.0.1:18080/original/orders/1 -H 'Authorization: Bearer demo-token'
curl -i http://127.0.0.1:18080/migrated/orders/1 -H 'Authorization: Bearer demo-token'

# 应用入口按配置选择唯一后端，响应的 X-Demo-Backend 显示归属。
curl -i http://127.0.0.1:18080/app/orders \
  -H 'Authorization: Bearer demo-token' -H 'Content-Type: application/json' \
  -d '{"item":"book","quantity":2}'

# 未迁移接口仍归原应用。
curl -i http://127.0.0.1:18080/app/legacy

# 使用 sold-out / quantity:0 / 缺少 Authorization 观察 409 / 400 / 401。
curl -i http://127.0.0.1:18080/migrated/openapi.json
```

停止当前 Demo 后，以 `make demo-app DEMO_MODE=rollback` 启动即可演示恢复原后端归属；`DEMO_MODE=original` 演示迁移前。每次重启都会重置两个内存仓库，因此此命令只演示路由配置回退，不能用于证明数据回退或在途请求无损切换。自动测试在同一个应用实例内完成 original → migrated → rollback，验证归属变化且未迁移接口保持可访问。

## 测试先行

本次先提交到工作区的是失败测试与返回 `migration demo not implemented` 的构造函数，实际运行确认红灯；随后实现请求链并重新运行测试。

| 测试 | 提前验证什么 |
|---|---|
| TestBehaviorParity | 成功创建、鉴权短路、validator 拒绝、业务冲突、读取/未找到、非法 path、畸形 JSON；比较状态、Content-Type、请求 ID、响应数据/文本和写入次数 |
| TestUnmigratedDelegatesOriginalApplication | 入口实际调用原 Nirvana 应用，不能用后端标记伪装归属 |
| TestSwitchAndRollback | 同实例的唯一后端归属切换、回退、未迁移接口和非法配置 |
| TestWriteFailureNeverFallsBack | 迁移后端业务失败时不会向原后端重放写请求 |
| TestCancelledContext | 原 handler 收到取消信号且无写入 |
| TestConcurrentRequests | 每后端并发 12 次写入，无重复/丢失副作用；配合 race 检查 |
| TestHTTPApplicationFlow | 临时真实 HTTP 服务中创建并读取同一条订单，检查已提交 Header 和 OpenAPI |
| TestContentNegotiationParity | 缺省/通配 Accept、JSON、不支持的 Content-Type 与 Accept |
| pilot 注册测试 | 无效签名、typed-nil handler、nil middleware、不支持 source、非法/重复路由整批拒绝，不留下已注册路由 |
| pilot Huma 集成测试 | Huma middleware 生效，OpenAPI 含实际响应 schema |

状态、JSON 和业务副作用采用精确比较；本例没有时间戳或随机 ID，无需忽略这些字段。`X-Demo-Backend` 是故意不同的观察标记，不计入行为一致性。执行顺序单独由内存 trace 检查，不在响应提交后修改 Header。

测试发现：锁定的 Nirvana 对 `{` 这样的畸形 JSON 返回 **500 文本 `unexpected EOF`**。本例明确保留并对照该历史行为；改变它应单独设计，不能把框架迁移当作默认修复。无 Accept Header 的请求也必须按原来的通配符规则工作。

## 实验边界

`internal/pilot` 是 受限的历史实验桥：在 Huma 路由和 middleware 后，构造 Nirvana HTTPContext、填充路径参数，再执行 Nirvana 自带的启动时编译 executor。原参数转换、operators、原 handler 和响应语义都由同一 executor 保留；没有代理请求到原服务。每次请求新建有可变游标的 middleware chain，避免跨请求共享状态。

当前验证的是这两个订单操作和显式列出的中间件；descriptor 继承由应用显式展开，未交付通用 descriptor 编译器、通用 DI 或完整支持矩阵。OpenAPI 只描述参数/响应基本类型，任意 operator 的自定义约束尚未转换；错误使用原格式，不走 Huma 默认错误格式。注册失败必须中止启动，公开稳定 API 尚未确定。

合成 Demo 的成功不能替代真实项目接口、全面兼容性、覆盖率 80% 或 P95 < 1ms 验收。本例不宣称这些目标已通过。

## 本次验证记录（2026-10-19）

环境：Go 1.23.12、Nirvana v0.3.1、Huma v2.35.0。

- 红灯：初始测试因 `migration demo not implemented` 失败。
- 红灯：增加调用次数断言后，未记录业务调用时失败；补充计数后能验证失败请求只调用迁移后端一次、原后端零次。
- 红灯：typed-nil handler 导致底层 executor panic，nil middleware 被错误接受；均已在注册入口显式拒绝。
- 绿灯：`make demo-tdd GOCMD=/opt/homebrew/opt/go@1.23/bin/go`，含 race 检查。
- CLI 冒烟：分别启动 original、migrated、rollback 模式，通过真实 HTTP 检查两个后端与应用入口的写入、未迁移接口归属及 OpenAPI；检查后终止本次创建的进程。

尚未交付真实项目接口验证、全量兼容支持及性能结论。

## T071 汇总（2026-10-22）

锁定 Go 1.23.12、Huma v2.35.0、官方 Nirvana v0.3.1。`go test -race -count=1 ./...` 通过：127 pass、1 skip（外部服务对照未给地址）。本目录 62 个测试（其中 3 个为独立原生 Huma 基线）全部通过。按当前 Nirvana 适配器判定的 60 项 Huma 能力：支持 0、部分支持 56、不支持 2（CBOR、merge-patch，均为 Nirvana 媒体准入 415）、尚未验证 0、不适用 2（Huma 其他 router adapter）。"部分支持"只代表列出的 case 通过。完整清单与依据见责任 spec 的 `contracts/compatibility.md`「T071」节。真实接口与性能验收仍开放。
