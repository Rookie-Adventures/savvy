# 蚂蚁链订单存证: logrus 全局污染 + 私钥文件部署位

蚂蚁链订单存证集成(蚂蚁链存证集成设计见 docs/...及 reference-antchain-evidence-design 记忆)首次 review 查出两个真问题。本文记 review 发现 + 修复 + 生产密钥落地。

## 症状(两个, 均非支付链阻断, 但会让存证在进生产时踩雷)

### A. RSA 私钥文件无部署位, 生产必崩
`service/antchain/client.go` 的 `Init()` 把 `ANTCHAIN_ACCESS_SECRET_FILE` 当作 SDK 配置里的 `AccessSecret` 字段塞进 tmp JSON。

追 SDK 源码 `restclient-go-sdk/utils/cert_util.go`:
- `NewRestClient()` 同步 `ioutil.ReadFile` 读配置 → `shake()` 握手
- `shake()` 调 `utils.Sign(plain, priKey)` → `getPrivateKey(priKey)` **又一次 `ioutil.ReadFile(priKey)`** 读私钥
- `retryableSendRequest` 在 token 失效(`Code=="202"`)时 **重调 `client.shake()`** → 私钥文件被反复读

结论: 私钥文件必须 **常驻** 在 `ANTCHAIN_ACCESS_SECRET_FILE` 指向的路径, 每次 token 刷新都读。开发机私钥在 `E:\mayilian\access.key` 跑得通, 生产容器里这路径不存在 → `Init()` shake 阶段 `getPrivateKey` 读文件失败 → `Enabled=false` 静默关闭, 存证不工作(支付不报错, 用户无感, 但冻资金申诉无存证硬证据)。

且失败信息只打 `"antchain: shake failed"`, 看不出是私钥文件缺、路径错、还是真握手失败, 排障费劲。

### B. SDK init() 污染 logrus 全局, 刷屏 stdout
`restclient-go-sdk/client/rest_client.go` 包 `init()`:
```go
log.SetFormatter(&log.JSONFormatter{})
log.SetOutput(os.Stdout)
log.SetLevel(log.InfoLevel)
```
`retryableSendRequest` 每次成功调用打一条 `log.Info("request and resp", param, resp)`。

fire-and-forget 存证 = `insertOrder` + `completeOrder` + `logOrder` 三步 = **每单 3 条 JSON info 日志直灌 stdout**, 绕过 new-api 自己的 `common.SysLog`。日志量随单数线性涨, 且不可控。

更隐蔽: 只要 `service/hermes.go` 的 `init()` import 了 antchain 包, SDK `init()` 就跑(Go 包 import 是编译期 + 传递依赖), logrus 全局被改 — **即使 `ANTCHAIN_ENABLED=false` 包不 Init, 副作用也已发生**。

## 根因

### A 根因
- SDK 配置 `AccessSecret` 字段语义是 **私钥文件路径** 而非私钥内容(命名极易误读成"密钥值")
- `Init()` 把 tmp 配置写完立刻 `defer os.Remove` 删 tmp **配置** 是对的(SDK 已同步读完); 但对私钥文件未做任何存在性校验, 生产拿不到清晰错误
- 无部署文档说明私钥该放哪

### B 根因
- antchain-go-sdk 自带一个 `init()` 直接改 logrus 包级全局, 假设它独占 logrus
- new-api 不用 logrus 做业务日志(用 `common.SysLog`), 但 SDK 的全局改动不会被 new-api 的 logger 接管, 直接漏到 stdout
- Go import 顺序: antchain 包 import SDK → SDK `init()` 先于 antchain 包 `init()` 跑 → antchain 包的 `init()` 看到的是 SDK **已经**改完的全局状态, 可在此还原

## 修复思路

### A
1. `Init()` 在 `NewRestClient` 之前 `os.Stat(ACCESS_SECRET_FILE)` 前置校验, 私钥文件不可达时打明确错误直接 return(不进 SDK, 不含糊 "shake failed")
2. docker-compose 给 new-api 加只读挂载 `-v /opt/savvy/secrets:/secrets:ro`, 私钥放宿主机 `chmod 600`, 不入镜像不入仓库
3. `.env.example` + docker-compose 写齐 10 个 `ANTCHAIN_*` env, 默认 `ANTCHAIN_ENABLED=false` 存证静默、支付照跑
4. 私钥文件 **不删**(区别于 tmp 配置): shake 重握手还要读

### B
在 antchain 包自己的 `init()` 里, SDK init 跑完之后, 把 logrus 全局还原到静默:
```go
func init() {
    logrus.SetOutput(io.Discard)
    logrus.SetLevel(logrus.WarnLevel)
}
```
- `io.Discard` 丢弃输出 → 不再灌 stdout
- `WarnLevel` 仅留警告及以上 → SDK 正常 info 日志闭嘴, 真有 warn 仍可被 recover 抓(若需要可改回可见 sink)
- import 顺序保证此 `init()` 跑在 SDK `init()` 之后, 还原有效; 即使 enabled=false 也中和副作用

## 改动清单

- `new-api/service/antchain/client.go`
  - `init()`: 还原 logrus 全局到 `io.Discard` + `WarnLevel`(B 修复)
  - `Init()`: 加 `os.Stat(ACCESS_SECRET_FILE)` 前置校验, 失败明确报错并 return(A 修复)
- `deploy/docker-compose.yml`
  - new-api service `volumes` 加 `- /opt/savvy/secrets:/secrets:ro`
  - `environment` 加 10 个 `ANTCHAIN_*` env(默认值兜底, 不填即静默)
- `deploy/.env.example`
  - 加 "蚂蚁链订单存证(可选)" 章节含全部 env 样例 + 启用 3 步骤

## 第二轮改动 (按 design.md 对账补齐)

读完 `docs/superpowers/specs/2026-07-18-antchain-order-evidence-design.md` 逐条对账, 补三处:

- `new-api/service/hermes.go`
  - 去掉 `if antchain.Enabled` 守卫, **无条件**装 `model.SubmitOrderEvidenceFn`(design.md line160 握手失败仍装钩子, 配错时运营期每单 `SysError` 逼排查, 不为握手失败切 noop)。`Enabled` 变量现仅 `TestShake` 用, 保留。
- `new-api/service/antchain/evidence.go`
  - `callContract` 里 `encoding/json.Marshal` 违 new-api JSON 铁律 → 换 `common.Marshal`(上轮漏的, 本轮逮到)
  - 末尾加回 `func demo()` 非导出自检(design.md line169 ponytail 惯例, 打印 input + nil-client 报错)
- `deploy/.env.example`
  - 蚂蚁链段补已知沙箱真值(AccessId=234`/KmsId/裸host RestUrl), 标注"裸 host 别带 /w3/api 后缀"与 SDK B 类认证说明, 方便直接拷

注: design.md line160 的"握手失败仍装钩子"与原 hermes.go"握不上就不装"冲突, 按文档为准收口。

## 验证

- `go build ./service/antchain/ ./service/ ./model/` → EXIT=0
- `go vet ./service/antchain/` → 干净
- 真值交叉对齐: Go 代码读的 10 个 `ANTCHAIN_*` 名与 docker-compose / .env.example 完全一致(grep 比对)
- `go test` 两包共 10 passed:
  - `TestSubmitEvidence_NoClient` (nil-client 即报错, 印证 line160 握手失败仍装钩子后运行期会报警)
  - `TestShake` `//go:build manual` 默认跳过
  - `TestCanonicalJSON_DataHash` (换 `common.Marshal` 后 dataHash 仍确定性, 字节一致 → 上链 hash 不漂)
  - `TestMoneyFenConversion_FloatPrecision` + `TestBuildTopupEvidence_MoneyFen` + `TestBuildSubscriptionEvidence_MoneyFen`
- 真链握手 `TestShake -tags=manual` **已跑通**:
  - env 必须用正名 `ANTCHAIN_*`(写成 `ANTHCHAIN_*` 多个 H 会被忽略, SysError 报 "missing required env" — 第一跑就栽这)
  - bash 里 Windows 反斜杠路径 `E:\mayilian\access.key` 走 `export VAR='...path'` 单引号防转义, 内联 `VAR=path cmd` 会被 bash 吃掉反斜杠
  - 私钥真文件是 `E:\mayilian\access.key`(28 行 1704B); `GoProject/.../restclient-go-demo/access.key` 是 3 行 demo 占位, 误用 → SDK 拒签
  - 结果: `[SYS] antchain: initialized successfully, contract=savvy-solidity` + `shake OK, token prefix: eyJhbGciOiJIUzI1NiJ9...`(JWT 真换到 token, PASS 1.14s)
  - 这步只验认证层(SDK RSA 签名 + shake 握手换 JWT token + RestUrl 裸 host 配法 + access.key 与 AccessId 配对), 不验合约 ABI 编码 / 上链 / 事件留痕
  - 关键收获: logrus `io.Discard` 还原没误伤握手(SDK 握手走 HTTP 不走 logrus 输出, 改 init 后照吐 token)

## 段2 (真上链验收) — 进行中

认证层已通, 段2 验的是段1 没碰的三层(责任不重叠, 不算走形式):

1. **ABI 编码层**: `insertOrder(string,string,string,string,string)` 5 个 string 参数能否正经打到合约, 而非签名通了但调用报错
2. **合约语义层**: `require(orders[tradeNo].tradeNo.compare_string(""))` 不被撞(同单重 insert 必 revert)、`completeOrder` 续写成功、`logOrder` 真发 `LOG_STRING` 事件
3. **真留痕层**: 蚂蚁链浏览器查到这笔 tradeNo 的区块号 + `LOG_STRING` 事件 — 这才证明整个集成端到端通

### 段2a 本地 E2E (2026-07-18, manual 测试裸跑, 不起 docker)

走 `go test -run TestSubmitEvidence_E2E -tags=manual ./service/antchain/`
- 测试见 `new-api/service/antchain/evidence_e2e_test.go`: 用真实 `model.BuildTopupEvidence(topup)` 构造 input → `SubmitEvidence` 三步打链。
- E2E 与 docker 验差异仅在触发点(E2E直调 vs 支付回调→goroutine); 三层核心一致。docker 还单独验注入点接线 + 沙箱, 留机B。

经 SDK 本地 check 边推进, 逐错暴露凭证真相:
- 第一错 `no bizid` — `utils/check_biz_param.go:19` SDK 本地前置拒空 bizId(demo 自部署合约能空跑, 正式合约必须带真 BizId)。design line45「bizId 留空」抄 demo 误判, 改。
- 用 `ANTCHAIN_BIZ_ID=a00e36c5`(REST URL 段 `/w3/api/{bizid}/{wt-token}` 推出)→ 本地 check 过, 网关 `code=41400 tenantid required`。
- 用 `ANTCHAIN_TENANT_ID=savvy`(盲猜 account 当 tenant)→ 过 tenant 校验, 网关 `code=204 account config invalid`。
- account 空 / `savvy` 同报 204 — 不是缺值, 是该 AccessId/KmsId 在网关侧**没配出对应链上账户**(控台账户管理未创建/绑定)。

### 段2a 卡点 — 合约语义层 onlyOwner (非代码 bug, 段2三层之一暴露)

放开 SDK logrus stdout (client.go init 临时改 stdout+Info) 抓出完整回包, 关键收获:
- 握手 token 内 JWT payload 明文: `"sub":"pnv3kEhXTWHRFGOY", "tenantId":"TWHRFGOY"`, **网关直接吐 TenantId=`TWHRFGOY`**(AccessId 后半段)。`ANTCHAIN_TENANT_ID=TWHRFGOY` 一试便过 41400, 里头 "savvy"纯属瞎猜蒙中早一层 check 幸运通, 非 tenant 真值。
- `account=savvy tenantid=TWHRFGOY bizid=a00e36c5 kmsId=7ysf...` 组齐后, insertOrder 回 `code=408 SERVICE_TX_VERIFY_FAILED result=120 gasUsed=0 logs=[]`。

`result:120` = 合约 revert 业务码(SERVICE_TX_VERIFY_FAILED 是合约执行 reverted 的网关具现)。对照 OrderEvidence.sol:
- `insertOrder/completeOrder/logOrder` 全 `modifier onlyOwner()` → `require(msg.sender == owner)`(L533)
- `owner = msg.sender` 由 constructor 设(L532), 即合约部署者账户。
- 当前 KmsId=7ysf...对应链上 identity 不等于 owner → onlyOwner require fail → revert → 120。

到这步段2验收面三层里 **ABI 编码层已证通**(请求打到了合约并被合约接收/执行/回业务码, 证实签名编码无问题), **合约语义层 onlyOwner 这层暴露**(留痕 L112 预言成真, 不是走形式)。剩 **真留痕层**(留痕 L113)未触到(还没一次成功 insert→complete→logOrder 跑通)。

可能根因 (二选一, 待控台核实):
- A. KmsId=7ysf2UgpTWHRFGOY1783011006931 在链上**未 CreateAccount 激活**, msg.sender nil/占位, 与部署 owner 不符, onlyOwner 挡。解:控台给该 KmsId 创建/激活账户(account 名即我传啥 susc)。
- B. **部署 `0x8d9ce16a...47f6703ed2907656e24bfc2c477f8f45` 的 owner 是另一个 KmsId/账户**(控台管理员账户), 这套 B 类存证凭证 7ysf... 根本没 onlyOwner 权限。解:换发 owner 凭证(私钥+KmsId+AccessId), 或放弃 owner-only 合约(合约已上链改不动, design L24 不动合约), 只能走复用 owner 凭证。

待用户去蚂蚁链开放联盟链控台查:
1. 当前 KmsId `7ysf2UgpTWHRFGOY1783011006931` 是否在链上 CreateAccount 激活过 → 控台「账户管理」看账户名 + 激活态
2. 合约 `0x8d9ce16a...47f6703ed2907656e24bfc2c477f8f45` 的部署 owner 是哪个 KmsId / 账户名(查这笔合约部署交易或控台业务详情)
  - 若 = 7ysf... 自己 → 问题在 A(account 未激活 / account 参数名填错), 给真 account 名后改 `ANTCHAIN_ACCOUNT=<真名>` 重试
  - 若 ≠ 7ysf... → 问题在 B, 拿到 owner 那套凭证(access.key+AccessId+KmsId)换进来

防扛(注):logrus 调试完已还原 init 到 `io.Discard + WarnLevel`, 不影响生产不刷屏。`io` import 恢复使用。

### 段2b 待办 (docker + 沙箱, 机B)

段2a 通后:
- `.env.example` 真值拷进实际 `.env` + 私钥放机B宿主机 `/opt/savvy/secrets/antchain-access.key` (chmod 600)
- `docker compose up -d --build new-api` → 看 `docker compose logs new-api` 出 `initialized successfully` 而非 `shake failed`
- 本地 secrets 软链/复制到 `E:/savvy/deploy/secrets/antchain-access.key` (gitignore 加 `secrets/`) + compose 卷改相对路径 `./secrets:/secrets:ro` (本地机B共用一行) + cloudflare 临时回调域名让沙箱能回本地 → 验注入点接线 + 沙箱跑通
- 支付宝沙箱充值一笔 → 同 tradeNo 去蚂蚁链浏览器查 `LOG_STRING` 事件
- 段2b 失败时排查面窄: 只剩注入点接线 / 沙箱回调, 三层+认证已排除

## 已知限制 / 尾巴

- **`io.Discard` 完全静默 SDK 日志**: 若将来要排查 SDK 握手/重试问题, 需临时改 `init()` 的 sink 到可见 writer(如 `os.Stderr` 或接 common logger)再看。保留这根线, 别删 `init()`。
- **`go.mod` 版本号 `v0.0.0` + replace `./third_party/restclient-go-sdk`**: vendor 兜着能编译跑, 但不是正经 semver。哪天官方仓库发版本想去掉 replace 走真版本, `v0.0.0` 拉不到, 需改真版本号并清 replace。当前不动。
- **`ANTCHAIN_ENABLED=false` 时 antchain 包仍被 import**: `service/hermes.go` 的 `init()` 无条件调 `antchain.Init()`, 即便 Enabled=false 也付出了 SDK init() + antchain init() 的开销(微秒级, 可忽略)。若要彻底零开销需 lazy import(动态加载), Go 无原生支持, 不值得。
- **私钥文件生命周期**: 宿主机 `/opt/savvy/secrets/` 不在 compose 卷里, 备份脚本 `deploy/backup.sh` 是否覆盖需单独确认 — 私钥不进备份没关系(可重新生成), 但**别误把 secrets 卷纳入自动备份上传到无加密处**。
- **两步写入中间状态**: insert 成功、complete 或 logOrder 失败时, 链上留半截, 无法补后续(合约 `require` 拒同 tradeNo 重 insert, complete 也拒空 tradeNo 的 complete)。降级接受, 仅 SysError 告警(design.md line182)。
- **shake token 过期**: SDK 自动 re-shake 仅在 `Code=="202"` 时触发(rest_client.go line270), 长跑若过期需重启或封装再加重握逻辑(design.md line183)。段2 未验。
