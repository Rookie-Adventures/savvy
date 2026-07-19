# 支付宝官方履约链路 —— 实测文档(2026-07-19)

**查询方式**: Exa 搜索官方文档(`mcporter call exa.web_search_exa --http-url https://mcp.exa.ai/mcp`,免 API key)+ Jina Reader 读全文。
**起因**: 商家后台发现「商品卖货 SAAS 服务商」配置面板 → 追问支付宝是否有官方"发货/履约"机制 → 发现 `alipay.open.mini.order.delivery.send` 订单发货接口 → 推翻"线上 SaaS 没有官方发货通道"的旧判断。

---

## 一、核心接口: 订单发货 `alipay.open.mini.order.delivery.send`

**官方文档**: https://opendocs.alipay.com/mini/07a169 (备: https://opendocs.alipay.com/mini/2eb7b522_alipay.open.mini.order.delivery.send)

### 权限集位置
`开发 > 服务端 > 私域产品 > 小程序产品 > 权限集列表 > 交易组件 > API 列表 > 小程序交易组件订单 > 订单发货接口`

→ **属于「小程序交易组件」权限集,不是通用当面付/电脑网站支付(支付产品线不同的族)**。

### 业务类型支持(关键)
- `merchant_biz_type = KX_SHOPPING`(电商购物): 只有 `PAID` / `PARTIAL_DELIVERED` 状态可发货
- `merchant_biz_type = VIRTUAL_RECHARGE`(**虚拟商品**): 只有 `PAID`(支付成功)状态可发货,其他返回"订单状态非法"

→ **支付宝官方明确支持虚拟商品充值有"发货"动作,SaaS 充值适用此类型**。

### 关键参数
| 参数 | 类型 | 必选 | 含义 |
|---|---|---|---|
| `finish_all_delivery` | Number | 必选 | 发货完成标志位,0=未发完 / 1=已发完 |
| `order_id` | string | — | 交易组件订单号 |
| `out_order_id` | string | — | 商户订单号 |
| `ship_done_time` | string | 条件必选 | 完成发货时间 yyyy-MM-dd HH:mm:ss(finish_all_delivery=1 时必传) |
| `delivery_list` | array | 条件必选 | 物流信息列表,**虚拟商品不需要传入** |
| `item_info_list` | array | 必选 | 商品信息(out_item_id / item_cnt 等) |

→ **虚拟商品发货超轻: 不传物流,只传时间 + 商品信息 + 完成标志位**。

### 关键业务错误码
- `ORDER_NOT_PAID` 订单尚未支付
- `ORDER_VALIDATE_ERROR` 订单前置校验失败,不满足发货条件
- `SEND_ORDER_IS_REPEAT` 重复完成发货
- `STATE_TRANSFER_ERROR` 状态推进失败
- `DELIVER_FORBID_FOR_HOLD` 禁止履约(拼团订单)

---

## 二、链路本质: 履约 → 结算(不是单纯"留痕")

来源: https://opendocs.alipay.com/solution/0d3q9a + https://opendocs.alipay.com/open-v3/07zofn

官方原文要点:
> 商家在订单状态关键节点变化时,调用以下接口实现对订单资金的结算... 订单发货:商家完成发货动作之后,调用 `alipay.open.mini.order.delivery.send` 同步订单发货。**虚拟商品类型在发货时 delivery_list 无需传参**。

> 通过小程序交易组件下单的订单,同步给订单中心时,只会同步订单支付成功的订单...

> 确认收货和发货是有前后依赖的接口,如果对一笔订单没有调用发货接口,则调用确认收货接口会失败。

### 完整履约状态机(虚拟商品简化版)
```
PAID (支付成功)
  ↓  调 alipay.open.mini.order.delivery.send (finish_all_delivery=1)
DELIVERED (已发货)
  ↓  按"虚拟商品结算周期"由支付宝结算
结算完成 → alipay.open.mini.order.settle.notify 向商家发结算通知
```

→ **这套接口本质是履约金融基础设施**: 发货 = 触发结算,不是单纯给你存个"我发货了"。
→ **比普通 `alipay.trade.*` 的 T+1 提现更硬**: 履约完成后支付宝按虚拟商品结算周期打款,资金解付与履约事实绑定。

### 异步通知可选订阅
- `alipay.open.mini.order.changed` 订单结果通知 → 收到 `status=PAID` / `DELIVERED` / `RECEIVED_CONFIRM` / `REFUND_CLOSED`
- `alipay.open.mini.order.info.changed` 订单信息变更 → `modified_info_type=RECEIVER_ADDRESS_INFO`

---

## 三、前置: 必须走「小程序交易组件」下单链路

`delivery.send` 的 `order_id`/`out_order_id` 仅在**交易组件订单**存在。普通 `alipay.trade.create`(电脑网站支付/当面付)下单的单**不在订单中心,无法调**。

### 商品建档(订单前必需)
**官方文档**: https://opendocs.alipay.com/mini/4880cf68_alipay.open.app.item.create
- 接口: `alipay.open.app.item.create`
- 关键参数: `out_item_id`(商家侧商品 ID,app_id 下全局唯一) / `title` / `category_id`(平台类目) / `sale_price` / `sale_status` / `skus`
- 商家后台「商品卖货 SAAS 服务商」配置面板 = 这个商品建档的图形界面(<https://opendocs.alipay.com/b/0cqcvg> 导航)
- 实操指南: https://opendocs.alipay.com/b/076cxe「普通商品提报指南」含类目申请、虚拟品类目、审核流程
- 重要门槛:
  - **必须先申请开通商品类目**,类目审核通过才能创建商品
  - **小程序未完成备案不能创建商品**(MINI_APP_HAS_NOT_REG 错误)
  - 虚拟商品(如手机充值)需在「运营中心-小程序信息-辅营类目」增加主营/辅营行业类目

### 下单
交易组件下单接口族 `alipay.open.mini.order.*`,产出 `order_id` + `out_order_id` 喂给 `delivery.send`。

---

## 四、对照 Savvy Agent 当前现状

### 当前 payment 集成
- 走普通 `alipay.trade.create` / `alipay.trade.wap.pay`(电脑网站支付 / 手机网站支付)
- 已合 dev(merge 4b8682a5f,9 task,17 commit)
- 这套链路 → 订单**不在交易组件订单中心**,无法调 `delivery.send`

### 要拿官方发货背书必须做
1. **改道**: payment 下单从 `alipay.trade.*` 切到 `alipay.open.mini.order.*`(小程序交易组件下单)
2. **商品建档**: 在商家后台或调 `alipay.open.app.item.create` 建商品拿 `out_item_id`
3. **类目开通**: 申请虚拟商品/充值类目(若未开)
4. **小程序备案**: Savvy 若用小程序,需先完成备案(你已有豫ICP2026026934,小程序备案是另一回事)
5. **回调改造**: 支付回调拿 `order_id`,加完 quota 后调 `delivery.send`(虚拟商品不传物流)
6. (可选) 订阅 `alipay.open.mini.order.changed` 拿 DELIVERED 状态通知

---

## 五、对前序链证存证方案的根本性影响

### 旧方案(2026-07-18 antchain-evidence-logrus-and-key-deploy.md / 段 2a)
- 上链付款侧 8 字段(tradeNo/userId/moneyFen/planId/provider/payTime/status/dataHash)+ bizType
- 设计待加 `deliverOrder` + `deliveredAt` + `deliveryHash` 上链"发货"侧
- 理由: 线上 SaaS 没有官方发货通道,自架链证做第三方不可篡改留痕

### 新事实推翻
- **支付宝官方 `delivery.send` 接口存在 + 支持虚拟商品**: 发货后支付宝订单系统内自带"何时发货"(ship_done_time)、"已履约"(DELIVERED)、"已结算"权威留痕
- **支付宝结算记录绑定履约事实**: 比自架链证更权威,且是支付宝系统内可被小二核查的官方证据
- **结论**: 有了官方背书,自架链证纯属冗余 → 全砍(见决策文档)

---

## 六、工具备忘

- Exa 本机可用(doctor 报 off 是 PATH 误报,实调为准):
  ```
  mcporter call exa.web_search_exa --http-url https://mcp.exa.ai/mcp query="..." numResults:8 --output json
  ```
- JPOM 编码坑: Windows 存文件读用 `encoding='utf-8-sig'`(有 BOM)
- PowerShell `curl` 被别名化为 Invoke-WebRequest,@要用 `curl.exe`,无 `sed`/`grep`,用 Select-String
- Jina Reader 读单 URL 全文免 key: `curl.exe -s "https://r.jina.ai/<URL>"`
