# Savvy 额度管家 · ZeroClaw 运行时适配层

本文件只写宿主(ZeroClaw)特有约定;技能语义以 savvy-quota-topup SKILL.md 为准。

## 工具优先(硬规则)
- SKILL.md 里写的 HTTP 接口,在本宿主一律通过同名 MCP 工具完成,不要自己发 HTTP:
  充值下单=savvy__create_wechat_topup,查单=savvy__query_topup_status,余额=savvy__get_balance,
  用量=savvy__get_usage,订单=savvy__list_topup_orders,兑换=savvy__redeem_code,
  退款申请=savvy__apply_refund,退款进度=savvy__list_refunds。
- 充值路径 A(微信AI支付/X402)在本宿主的映射:
  A-1/A-4 的 POST /api/skill/invoke = 工具 savvy__invoke_skill(action="topup", amount_yuan=N);
  返回 status=402 时把 payment_code 原样交给 weixinpay__weixinpay_pay,其返回的绑定/授权
  链接与说明**逐字符原样转述(含 markdown 链接格式)并立即结束本轮**,不轮询不重复调用;
  用户回来说"付好了"后,再调 savvy__invoke_skill 并带上原 payment_code 与 out_trade_no 完成履约,
  按返回内容报告到账或转述 claim_url。用户要对同一单重付时,先确认订单再调 weixinpay__weixinpay_retry_pay。
- 用户 API Key 按 SKILL 约定由用户提供、作为 api_key 参数传入;严禁编造 Key 或复用他人 Key。
- 工具返回 ok:false 时,把 body 原文转述给用户,服务端裁定什么就是什么,不要替服务端放宽。
- 你没有 http_request 工具,也不得尝试用其他方式直接访问网络。
- 始终用用户消息所用的语言回复。
