# 蚂蚁链订单存证集成设计

日期: 2026-07-18
分支: feature/antchain-integration

## 目的

支付宝/微信对 SaaS 订阅(类虚拟商品)有风控,日流水预期 100 万时随时可能冻资金要交交易凭证申诉。蚂蚁链存证 = 不可篡改时间戳 + 司法可信存证渠道,申诉时甩「此订单 X 时上链,对应支付商回调 Z」是对抗扯皮最有力的硬证据。

证据记忆 `reference-antchain-evidence-design` 选定用法 A(订单存证)、撤销 B/C。本设计把登记备料落地为实做。

边界:支付链不依赖链。上链失败不影响订单成功,fire-and-forget + 告警日志,不重试。

## 合约(已部署,不动)

- 合约名: `savvy-solidity`(编译目标名)
- 合约地址: `0x8d9ce16a998bfebc5f5b168122a2621b47f6703ed2907656e24bfc2c477f8f45`
- 源码: `docs/solidity/OrderEvidence.sol`
- 写入函数:
  - `insertOrder(string tradeNo, string userId, string moneyFen, string planId, string provider)` — 写前 5 字段
  - `completeOrder(string tradeNo, string payTime, string status, string dataHash, string bizType)` — 写后 4 字段
- 读取函数: `getTradeNo/getUserId/getMoneyFen/getPlanId/getProvider/getPayTime/getStatus/getDataHash/getBizType(string tradeNo) → string`
- 诊断函数 `debugLengths` 不管(链上已部署删不了,正式代码不调用)。
- 两步写入语义约束:`insertOrder` 要求该 tradeNo 空,`completeOrder` 要求非空。中间状态(已 insert 未 complete)是已知尾巴,接受。

## SDK 与认证

采用官方 Go SDK `restclient-go-sdk`(方案 A),路径 `E:\mayilian\restclient-go-sdk\restclient-go-sdk`,demo 在 `E:\mayilian\GoProject\src\gitlab.alipay-inc.com\antchain\restclient-go-demo\demo.go`。

SDK 走 B 类认证(AccessId + AccessSecret RSA 私钥 + shake 握手换 token),不开 wt-token 那套 REST URL 拼路径方式。`access.key` 文件内容作为 `AccessSecret`。

核心调用:
```go
restClient.CallContract(
    bizId,        // 留空
    orderId,      // 链下唯一键,自定义
    account,      // "savvy"
    tenantId,     // 留空(demo 同款)
    contractName, // "savvy-solidity"
    methodSignature,                  // "insertOrder(string,string,string,string,string)"
    inputParamListStr,                // JSON 数组字符串,如 `["a","b","c","d","e"]`
    outTypes,                          // 写传 `[]`,读取传 `["string"]`
    kmsId,                             // 7ysf2UgpTWHRFGOY1783011006931
    isLocal,                          // false=写入上链, true=本地只读
    gas,                               // 默认值
)
```
SDK 自动握手(构造时 `shake()`)、自动 ABI 编码,调用方不传 calldata。

## 凭证

- Account: `savvy`
- KmsId: `7ysf2UgpTWHRFGOY1783011006931`
- AccessId: `pnv3kEhXTWHRFGOY`
- AccessSecret: `E:\mayilian\GoProject\...\access.key` 文件内容(RSA 私钥)
- RestUrl: `https://rest.baas.alipay.com`
- BizId / TenantId / wt-token: 本路径不用,留空/忽略(wt-token 属另一套开放联盟链接口,本 SDK 不支持)。

凭证走环境变量/文件,不进 git。

## 触发范围

订阅订单 + 钱包充值两条路径都上链。

- 订阅: `model/subscription.go:614`(事务提交后,`CompleteSubscriptionOrder` 内 `:598` 状态翻转 + `:606` tx.Save 之后,`:618 upgradeGroup` 分支之前)。
- 充值支付宝: `controller/topup_alipay.go:126`(`topup.Status = common.TopUpStatusSuccess` 之后)。
- 充值易支付(含微信/支付宝多渠道): `controller/topup.go:392`(状态翻转之后)。

每处注入点尾部加异步调用,照 `NotifyManagerUpgradeFn` 模式:
```go
if SubmitOrderEvidenceFn != nil {
    go func(in SubmitOrderEvidenceInput) {
        if err := SubmitOrderEvidenceFn(in); err != nil {
            common.SysError("antchain evidence submit failed: " + err.Error())
        }
    }(buildEvidenceInput(order))
}
```
goroutine 内失败仅 `common.SysError`,不阻塞回调响应。支付链不依赖链。

## 新增组件(3 文件,不扩接口)

- `new-api/model/hermes_notify.go` 旁加包级函数变量 `var SubmitOrderEvidenceFn func(in SubmitOrderEvidenceInput) error`(模式照抄同文件 `NotifyManagerUpgradeFn`)。`SubmitOrderEvidenceInput` 结构体定义于此或 model 包,字段见下节。
- `new-api/service/antchain/client.go` — 封装 RestClient:启动构造(读 env + 读 access.key 文件 → NewRestClient → shake)、运行期复用。单实现,不为单实现造 interface(ponytail: 单实现不抽 interface)。
- `new-api/service/antchain/evidence.go` — `SubmitEvidence(in SubmitOrderEvidenceInput) error` 实现,即 `SubmitOrderEvidenceFn` 的注入值。组装参数 → CallContract insertOrder → CallContract completeOrder → (可选)读 getTradeNo 自检。末尾留 `func demo()` 自检(ponytail 自验惯例,非导出)。

main 启动调一次 `antchainInit()` 注入 `model.SubmitOrderEvidenceFn = antchain.SubmitEvidence`,照 `service/hermes.go:585` 的 `model.NotifyManagerUpgradeFn = NotifyManager` 模式。

## 数据流

```
支付回调验签成功
  → CompleteSubscriptionOrder / 充值状态翻转(事务提交)
  → 返回 HTTP 200 给支付商
  → (并行 goroutine) SubmitOrderEvidenceFn(input)
       → antchain.SubmitEvidence
           → client.CallContract(insertOrder, [tradeNo,userId,moneyFen,planId,provider], isLocal=false)
           → client.CallContract(completeOrder, [tradeNo,payTime,status,dataHash,bizType], isLocal=false)
           → (可选) client.CallContract(getTradeNo, [], ["string"], isLocal=true) 自检回读
       → 失败 common.SysError,不重试
```

## 字段映射

链 `Order` 字段 ← new-api 来源:

### 订阅(SubscriptionOrder, model/subscription.go:214)
- `tradeNo` ← `order.TradeNo`
- `userId` ← `strconv.Itoa(order.UserId)`
- `moneyFen` ← `strconv.FormatInt(int64(math.Round(order.Money*100)), 10)`(float64 元→分,round 防浮点误差)
- `planId` ← `strconv.Itoa(order.PlanId)`
- `provider` ← `order.PaymentProvider`(alipay/wechat)
- `payTime` ← `order.CompleteTime` 格式化为 RFC3339 字符串(`time.Format(time.RFC3339)`),含时区可读可验证
- `status` ← 固定 `"SUCCESS"` 字符串(此处上链即订单已成功,Itoa 数字串对证据无意义)
- `dataHash` ← `SHA-256(canonicalJSON(order))` 十六进制(整单签名,防个别字段被换)
- `bizType` ← `"subscription"`

`insertOrder` 传前 5(tradeNo,userId,moneyFen,planId,provider)。`completeOrder` 传(tradeNo,payTime,status,dataHash,bizType)。

### 充值(TopUp, model/topup.go)
- `tradeNo` ← `topup.TradeNo`
- `userId` ← `strconv.Itoa(topup.UserId)`
- `moneyFen` ← 同上转分(用 `topup.Money` 或 `Amount`)
- `planId` ← 空串(充值无套餐)
- `provider` ← `topup.PaymentProvider`
- `payTime` ← `topup.CompleteTime` RFC3339 字符串
- `status` ← `"SUCCESS"`
- `dataHash` ← `SHA-256(canonicalJSON(topup))`
- `bizType` ← `"topup"`

`canonicalJSON`: 字段按固定顺序序列化(如 key 字典序),只在 new-api 端算,链下后续扯皮验证用同一算法。dataHash 算法不变即可,链上只存一个十六进制串。

## 配置与环境

不硬编码 GOPATH(demo 那样),改读 env 或 `constants.go`:
- `ANTCHAIN_ACCESS_ID` = access-id
- `ANTCHAIN_ACCESS_SECRET_FILE` = access.key 文件路径(生产容器内挂载)
- `ANTCHAIN_REST_URL` = https://rest.baas.alipay.com
- `ANTCHAIN_KMS_ID` = KmsId
- `ANTCHAIN_ACCOUNT` = savvy
- `ANTCHAIN_CONTRACT_NAME` = savvy-solidity
- `ANTCHAIN_BIZ_ID` = 留空
- `ANTCHAIN_TENANT_ID` = 留空
- `ANTCHAIN_ENABLED` = true/false(默认 false;配置缺或 false 时 `SubmitOrderEvidenceFn` 保持 nil,注入点 `if != nil` 自然跳过 — 零侵入,开发环境不瞎上链)
- `ANTCHAIN_GAS` = 默认值(显式可配)

key 走 env/file,绝不进 git。

## 错误处理

- 支付回调主路径不接触链 — goroutine 隔离。回调 handler 已先返回 HTTP 200。
- shake 握手失败(启动时): `common.SysError("antchain shake failed")`。仍注入 `SubmitOrderEvidenceFn`(运行期每次调失败走告警),不为握手失败切 noop — 简单。
- insertOrder 失败: 不继续 completeOrder,`SysError` 一条,不上报支付。
- completeOrder 失败: 链上留中间状态(insert 已成功),`SysError`。下次同 tradeNo insertOrder 会被合约 `require(orders[tradeNo].tradeNo.compare_string(""))` 拒(revert),无法补 completeOrder — 两步设计已知尾巴,接受(订单已成功,证据不全属可接受降级,fire-and-forget 不重试)。
- 告警集中走 `common.SysError`(项目现有唯一日志渠道,不引新机制)。

## 测试

- `antchain/client_test.go`: 真 `access.key` + 真 RestUrl 跑 `NewRestClient` 验 shake 通路。联网,标 `//go:build manual` 或 `t.Skip`,默认 `go test ./...` 跳过,手动 `go test -run TestShake -tags=manual` 触发。
- `antchain/evidence_test.go`: `SubmitEvidence` 字段映射逻辑表驱动测(moneyFen 转换、dataHash 算法、bizType 选择),纯函数不联网。
- `evidence.go` 末尾 `func demo()` 自检(非导出,字段组装逻辑纵观)。

## 不做(YAGNI)

- 不做链上状态机业务(B)、不做资产上链(C)— 记忆已撤。
- 不做重试队列/定时补登 — 接受两步设计中间状态尾巴。
- 不做链上读取 API 暴露给前端/管理后台 — 看证据走蚂蚁链浏览器,不自建查链接口。
- 不动链上已部署合约,不管 debugLengths。
- 不引新事件总线 — 直接复用 `NotifyManagerUpgradeFn` 模式。
- 不开 wt-token 那套 REST 拼路径接口 — 本 Go SDK 不支持,A/B 类二选一已选 B。

## 限制与已知尾巴

1. 两步写入中间状态:insert 成功 complete 失败时,链上留半截,无法补 complete(合约 `require` 拒同 tradeNo 重 insert)。降级接受,仅告警。
2. shake 握手过期:SDK token 有有效期,运行期长时若过期需 SDK 自动重握或重启。实现期验证 SDK 是否自动 re-shake,不自动则在 client 封装加重握逻辑。
3. dataHash 算法一旦上链即锁,后续链下验证必须用同一 canonicalJSON 实现,改算法等于断证据链。在 `evidence.go` 注释明示此约束。

## 实现顺序提示(留给 writing-plans)

1. env + 配置读 + access.key 文件读
2. client.go(RestClient 封装 + shake)
3. SubmitOrderEvidenceInput 结构体 + 注入变量
4. evidence.go(字段映射 + insertOrder + completeOrder + 可选自检)
5. main 注入
6. 三处注入点订阅/充值支付宝/充值易支付接 fire-and-forget
7. 测试
