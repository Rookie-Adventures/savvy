# 支付宝官方履约链路 vs 自架链证 —— 决策记录(2026-07-19)

**状态**: ⚠️ 决策已反转。下方"采纳路线B"段为初稿,于 2026-07-20 否决。最终决策见文末「最终决策(2026-07-20 反转)」。

---

## 决策核心

**采纳路线 B**: 接入支付宝官方履约链路(`alipay.open.mini.order.delivery.send` + 小程序交易组件下单),放弃自架链证存证方案。

**理由**: 支付宝官方接口本身就是第三方权威背书 —— 发货后支付宝系统内自带"何时发货 / 已履约 / 已结算"权威留痕,可被核查小二直接核验,且绑定了资金结算。自架蚂蚁链存证在此场景下纯属冗余,没有比这更权威的来源了。

---

## 砍除清单(不再做)

- ❌ 合约 `OrderEvidence.sol` 新增 `deliverOrder` 函数 —— 不做
- ❌ 新增字段 `deliveredAt` / `deliveryHash` —— 不做,设计作废
- ❌ Go `evidence.go` 新增 `DeliverOrder` 调用 + `deliverJSON` 构建 + `deliveryHash` 计算 —— 不做
- ❌ E2E 加"付款后补一步调 DeliverOrder 断言" —— 不埋
- ❌ **段 2a 已落地的付款侧 8 字段链证**(tradeNo/userId/moneyFen/planId/provider/payTime/status/dataHash+bizType)—— 不再维护(已有代码暂留,不再追加)
- ❌ 段 2b 原计划"沙箱跑当前字段端到端后补字段" —— 不再追加字段,沙箱跑通 = 收尾

**保留**(不动):
- `docs/records/2026-07-18-antchain-evidence-logrus-and-key-deploy.md`(段 2a 技术留痕,历史不删)
- 上一份 `2026-07-19-alipay-official-delivery-channel.md`(官方履约链路实测文档)
- deploy/.env.example 真值填充(沙箱真值已填,留作历史)

---

## 接下来要做的真活(路线 B)

### 必须先搞清的前置(用户尚未理清)
1. ~~payment 是否需要整体从 `alipay.trade.*` 切到 `alipay.open.mini.order.*`(小程序交易组件)~~ —— 已确认需要切
2. Savvy 是否需要小程序? 小程序是否已备案?(delivery.send 属小程序交易组件权限集)
3. 商品类目(虚拟充值)是否已申请开通?
4. 小程序交易组件下单对商家前端集成的影响(支付体验是否变化、H5/PC 端如何拉起)

### 已明确的执行路径
1. 商品建档: 商家后台「商品卖货 SAAS 服务商」面板 或 调 `alipay.open.app.item.create`,拿 `out_item_id`
2. payment 下单改道: 切小程序交易组件下单(`alipay.open.mini.order.*` 族),回调拿 `order_id`/`out_order_id`
3. 履约回调: 支付成功 → savvy 给用户加 quota → 同时刻调 `alipay.open.mini.order.delivery.send`(`finish_all_delivery=1` + `ship_done_time`,虚拟商品不传 `delivery_list`)
4. (可选) 订阅 `alipay.open.mini.order.changed` 拿 `DELIVERED` 状态通知做内部对账

### 不在本次范围
- 支付宝官方账单导出(申诉时从商家中心导出带签章 PDF 做线下第三方证据)—— 无需代码,被核查时导出即可

---

## 历史脉络(供返查)

- **2026-07-07**: 蚂蚁链订单存证字段设计登记(reference-antchain-evidence-design.md),用法 A 订单存证,冻资金申诉硬证据
- **2026-07-18**: 蚂蚁链存证段 2a 真链 E2E 写三步全 PASS(tradeNo=E2E-1784465681),卡凭证授权层
- **2026-07-19 早**: 讨论如何证明"发了货" → 设计补 `deliverOrder`+`deliveredAt`+`deliveryHash`
- **2026-07-19 中**: 用 Exa 搜支付宝官方文档 → 发现 `alipay.open.mini.order.delivery.send` 支持虚拟商品发货 + 履约结算 → 推翻"线上 SaaS 无官方发货通道"判断
- **2026-07-19 本决策**: 链证全砍,改走支付宝官方履约链路

---

## 待用户理清的问题(继续讨论时的起点)

1. 路线 B 切支付产品线,工作量大,是否值得现在就切?(vs 先把当前 payment 跑稳)
2. 小程序交易组件对 PC/H5 端用户支付体验的影响(用户是否要从 scheng.net 跳小程序?还是有 H5/PC 的等价组件)
3. 商品建档的"灵活定价"机制能否满足充值金额不固定(SaaS 充值传任意金额)
4. 类目开通审核周期 + 备案要求,对上线时间的影响
5. 切换后,既有依赖 `alipay.trade.*` 的代码面余额/订阅付款链路是否会断,迁移成本

---

# 最终决策(2026-07-20 反转)

## 反转: 采纳路线 1 —— 回自架蚂蚁链链证存证,路线 B 否决

上方"采纳路线 B / 全砍链证"的判断作废,**回到自架链证做履约证据**。

### 否决路线 B 的决定性原因

**关键阻断点不在"能不能开发",在"虚拟充值类目能不能开"——而这个答案是"不能也不该碰"**:

1. `delivery.send` 的 `merchant_biz_type` 只支持两类:
   - `KX_SHOPPING` 电商购物(实物有物流,与 SaaS 充值场景不符)
   - `VIRTUAL_RECHARGE` 虚拟商品(等于要走**虚拟充值类目**)
2. **虚拟充值类目是支付宝严管高发区**(话费/Q币/卡密类,洗钱风险高),审核严苛。
3. Savvy 的场景"SaaS 充值跑自己平台模型"对应到支付宝类目体系应落**软件开发服务**类目 —— 而软件开发服务**不在 delivery.send 认的两类 bizType 里**。
4. 即使强行去开虚拟充值类目,要么过不了审、要么被认定为洗钱通道擦边——自找不痛快,合规风险高。

**用户(粟城科技负责人)实务判断**: "软件开发服务类,没必要自己去找不痛快。"——不去碰虚拟充值类目(2026-07-20 明确)。

**结论**: 路线 B 对 Savvy 场景不可行(类目卡死),或可行但代价超过收益(强开虚拟充值类目踩合规雷)。

### 撤销上方"砍除清单"——解禁回链证

✅ **恢复执行**(撤销上文砍除清单):
- ✅ 合约 `OrderEvidence.sol` 新增 `deliverOrder` 函数 —— 恢复
- ✅ 新增字段 `deliveredAt` / `deliveryHash` —— 恢复
- ✅ Go `evidence.go` 新增 `DeliverOrder` 调用 + `deliverJSON` 构建 + `deliveryHash` 计算 —— 恢复
- ✅ E2E 加"付款后补一步调 DeliverOrder 断言" —— 恢复
- ✅ 段 2a 付款侧 8 字段链证 —— 维护,发货侧 2 字段补上

### 撤销上方"路线 B 真活"——不再做

- ❌ 不创建小程序应用 / 不做小程序交易组件下单 / 不调 `delivery.send`
- ❌ 不为履约迁支付体验到小程序
- ❌ payment 继续 `alipay.trade.*` 普通网页收款(已合 dev 4b8682a5f)
- payment 改造仅限: 在回调链路里加 fire-and-forget 调链证 deliverOrder

### 兜这圈不掉链的证据(留存的真知识)

官方履约链路实测文档两篇(`2026-07-19-alipay-official-delivery-channel.md` / `2026-07-19-alipay-appeal-docs.md`)留作历史留痕不删除,记录的核心事实:
- 支付宝官方确有 `delivery.send` + 履约结算背书,但强绑小程序交易组件
- 对 Savvy"软件开发服务"场景,**无适用的官方履约背书通道**
- scheng.net 网页收款档下,自架蚂蚁链链证 = 唯一第三方不可篡改证据来源

**这圈的价值**: 把"官方有没白吃的履约背书"彻底查清。结论:对你场景没有适用的;心安理得回链证。下次任何人(包括未来的自己)疑"是不是漏了官方更省的路"——看这两篇知道查过、放弃了什么、为什么。

---

## 段 2b 待执行项(回链证后)

1. 改 `OrderEvidence.sol`: struct Order 末尾加 `deliveredAt` / `deliveryHash`;加 `deliverOrder(tradeNo, deliverJSON)` 函数(onlyOwner + require 订单已存在 + emit LOG_STRING)
2. 改 Go `evidence.go`: 加 `DeliverOrder` 调用 + `deliverJSON` 构建 + `deliveryHash` SHA-256(tradeNo + deliveredAt)
3. 改 E2E `evidence_e2e_test.go`: 付款后补一步调 `DeliverOrder`,断言链上有发货记录
4. payment 回调链路加 fire-and-forget 调 `DeliverOrder`(给 quota 后同发,不影响主流程)
5. 段 2b 沙箱跑 E2E: 付款 8 字段 + 发货 2 字段 全流程验证
6. 砍掉的"买方 buyer / 退款 refund" 字段维持不加(YAGNI: 买方身份走支付宝官方账单线下,退款侧等遇到再说)

---

## 段 2b 实施落定 (2026-07-20 代码开干)

代码已落地(本地编译 + go test 全绿, 真链 E2E 待部署合约后跑):

1. **合约** `docs/solidity/OrderEvidence.sol`: struct Order 加 `deliveredAt`/`deliveryHash`; 加 `deliverOrder(tradeNo, browserJson, deliveredAt, deliveryHash)` 函数(onlyOwner + require 订单已存在 + 写 struct + emit LOG_STRING); 加 getter `getDeliveredAt`/`getDeliveryHash`。
2. **Model** `new-api/model/antchain_evidence.go`: 加 `DeliverEvidenceInput` + `BuildDeliverEvidence(tradeNo, deliveredAt)`。`deliveryHash = SHA-256(canonicalJSON({tradeNo,deliveredAt}))` 二英文 key 锁(与付款 dataHash 解耦)。易读层选 B: prettyJSON 出英文 3 key 后剥尾 `\n}` 插入中文备注段, 中文层不进 canonicalJSON(deliveryHash 算完即锁)。
3. **Service** `new-api/service/antchain/evidence.go`: `SubmitEvidence` 尾串第四步 `DeliverOrder`; 导出 `DeliverOrder(tradeNo)` 独立函数(deliveredAt 取 goroutine 执行时刻 RFC3339)。
4. **payment 回调 4 点未改**: 复用现有异步 `SubmitOrderEvidenceFn(BuildTopupEvidence/...)` 一处调用, 内部已串四步 → 4 回调点零改动。比计划第4项更省。
5. **E2E** `evidence_e2e_test.go`: 付款三步 PASS 后补 `DeliverOrder` 第四步断言。
6. **Test** `antchain_evidence_test.go`: 加 `TestBuildDeliverEvidence` — hash 确定性 + DeliverJSON JSON 合法性(剥尾拼接易错, 用 common.Unmarshal 验) + 中文易读层 key 在。
7. 砍掉的 buyer/refund 维持不加。

### 真链 E2E 前置(留痕, 未做)
- **合约须重新部署到 BaaS**: 旧链上合约无 `deliverOrder` 函数, 调用必 revert。E2E 跑前先重发合约(`OrderEvidence.sol` 改了 struct+函数)。
- 重发后合约地址/abi 可能变: 确认 `ANTCHAIN_CONTRACT_NAME` 指向新发合约名, 验 `insertOrder`/`completeOrder`/`logOrder`/`deliverOrder` 四函数都在。
- 重试队列(方案三提及)未建: 现 fire-and-forget 失败仅 SysError, 无 DB 兜底重发。YAGNI: 等真出现漏调再做(段2b plan 第4项说回 DB 队列, 决策文档第4项说 fire-and-forget 即可, 取后者更省路)。
