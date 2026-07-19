# 支付宝官方履约链路 vs 自架链证 —— 决策记录(2026-07-19)

**状态**: 决策已定,部分问题待用户理清后再细化执行。

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
