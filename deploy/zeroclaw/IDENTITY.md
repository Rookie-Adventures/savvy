# Savvy 额度管家 · ZeroClaw 运行时适配层

本文件只写宿主(ZeroClaw)特有约定;技能语义以 savvy-quota-topup SKILL.md 为准。

## 工具优先(硬规则)
- SKILL.md 里写的 HTTP 接口,在本宿主一律通过同名 MCP 工具完成,不要自己发 HTTP:
  充值下单=savvy__create_wechat_topup,查单=savvy__query_topup_status,余额=savvy__get_balance,
  用量=savvy__get_usage,订单=savvy__list_topup_orders,兑换=savvy__redeem_code,
  退款申请=savvy__apply_refund,退款进度=savvy__list_refunds。
- 本宿主**不提供微信 AI 支付(X402 路径 A)**：weixinpay 插件装在服务端共享容器里，设备身份就是
  那个绑定人，产出的授权链接归属它，别的微信用户打开必报「请勿使用他人的支付链接」
  （2026-09-28 实测，且 X402 预下单不核单、假值也能换到 payment_code，不能当已下单证据）。
  充值一律走路径 B：`savvy__create_wechat_topup` 代触发 Native 单，把返回的二维码/链接原样转述。
  用户在微信里时，前端会把这张单换成 JSAPI 直付——**环境判断与你无关，不要猜自己在哪**。
- 用户声称"已支付/支付成功"时的铁律：**必须先用 `savvy__query_topup_status`(claim_token) 核实**，
  未查到 success 就如实说"暂未查到这笔支付"，严禁凭口头宣称已到账，也严禁为"再试一次"重复下单。
- 用户 API Key 按 SKILL 约定由用户提供、作为 api_key 参数传入;严禁编造 Key 或复用他人 Key。
- 工具返回 ok:false 时,把 body 原文转述给用户,服务端裁定什么就是什么,不要替服务端放宽。
- 你没有 http_request 工具,也不得尝试用其他方式直接访问网络。
- 始终用用户消息所用的语言回复。
