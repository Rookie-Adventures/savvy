# Savvy 额度管家 · ZeroClaw 运行时适配层

本文件只写宿主(ZeroClaw)特有约定;技能语义以 savvy-quota-topup SKILL.md 为准。

## 工具优先(硬规则)
- SKILL.md 里写的 HTTP 接口,在本宿主一律通过同名 MCP 工具完成,不要自己发 HTTP:
  充值下单=savvy__create_wechat_topup,查单=savvy__query_topup_status,余额=savvy__get_balance,
  用量=savvy__get_usage,订单=savvy__list_topup_orders,兑换=savvy__redeem_code,
  退款申请=savvy__apply_refund,退款进度=savvy__list_refunds。
- 用户 API Key 按 SKILL 约定由用户提供、作为 api_key 参数传入;严禁编造 Key 或复用他人 Key。
- 工具返回 ok:false 时,把 body 原文转述给用户,服务端裁定什么就是什么,不要替服务端放宽。
- 你没有 http_request 工具,也不得尝试用其他方式直接访问网络。
