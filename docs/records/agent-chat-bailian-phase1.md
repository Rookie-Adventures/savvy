# 百炼智能体对话下单 Phase 1(体验版)落地记录

## 现象/目标
用户希望在主站对话中直接创建充值/订阅订单(对话即下单)。

## 方案
百炼智能体(应用 ID cb7afba7...)+「AI 支付(体验版)」MCP;
new-api 新增 /api/user/agent/chat 转发接口(配置 Host/Key/AppId 走 OptionMap);
前端 /agent-chat 页复用 ai-elements 聊天组件,回复中的 alipay 链接渲染为带协议勾选的支付卡片。

## 改动清单
- setting/operation_setting/agent_bailian.go(配置项)
- model/option.go(落库注册)
- service/dashscope_agent.go(百炼 HTTP 调用,60s 超时)
- controller/agent_chat.go + router(POST /api/user/agent/chat,CriticalRateLimit)
- web features/agent-chat(聊天页+支付卡片+api+pay-links)
- 路由/侧边栏/模块开关/i18n 六语言
- hooks/use-sidebar-config.ts(侧边栏配置闭环第四处注册,Task 6 审查修复,commit e6c8145920)

## 验证
自动化(2026-08-31,分支 feature/agent-chat-payment,commit e6c8145920):
- go test ./setting/... ./controller/... 全部 ok;service 包仅存量失败 2 项
  (TestObserveChannelAffinityUsageCacheByRelayFormat_MixedMode / _UnsupportedModeKeepsEmpty,
  在特性基点 21c78315bb 同样失败,与本特性无关;单独运行时通过,系包内测试共享状态污染)
- 本特性新增 5 项测试全部 PASS:TestIsAgentBailianConfigured、TestAgentChatSessionRoundTrip、
  TestAgentChatUpstreamError、TestAgentChatRejectsUnconfigured、TestAgentChatRejectsEmptyPrompt
- go build ./... 成功;web/default npm run build 成功

- 待执行(部署时由管理员操作):
  - Step 3 配置三项:经 option API 写入 AgentBailianHost/AgentBailianAppId/AgentBailianKey,重启容器生效
  - Step 4 手工验收(0.01 元体验版):勾选门禁/支付链接/查询确认/多轮会话,补全流程截图与结论

## 限制
- 体验版资金进测试商户,不可提现;正式收款需企业支付宝扫码签约换生产密钥
- 非流式输出;支付成功不加钱包额度(记账桥是 Phase 2)
- 无管理后台表单,配置经 option API + 重启生效

## 尾巴
- Phase 2:AI 收正式签约、记账桥(轮询查询支付→加额度)、userId 透传、流式
- 上线前轮换泄露过的 DASHSCOPE key
