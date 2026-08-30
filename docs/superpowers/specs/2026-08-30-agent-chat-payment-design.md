# 百炼智能体对话下单(AI 收·体验版)集成设计

日期:2026-08-30
状态:已确认(用户批准核心决策)

## 1. 目标与背景

在 new-api 主站新增独立页面,用户与「百炼智能体」对话即可发起充值/订阅订单:
智能体挂载支付宝官方「AI 支付(体验版)」MCP 服务,对话流中返回支付链接,
主站前端渲染为带协议勾选的支付卡片,完成「对话即下单」体验验证。

- 售卖对象:钱包充值 + 订阅套餐(两者都要)
- 落点:new-api 主站(不是 hermes-workspace)——充值/订阅/支付卡片资产全在主站
- 阶段:Phase 1 仅验证链路(体验版测试商户,资金不进生产账户);Phase 2 记账桥后置

## 2. 关键事实(已验证)

- 收款智能体必须出生在百宝箱/百炼/Coze/魔搭之一,自建 Hermes Agent 不可用官方 AI 收
- 百炼「AI 支付(体验版)」= 支付宝官方支付 MCP Server 云端部署,5 工具:
  创建支付/查询支付/退款/退款查询(金额与标题由智能体自由控制)
- 体验版预置测试商户密钥,0.01 元测试,资金不可提现,不碰生产商户配置
- 智能体已创建并发布,应用 ID `cb7afba7673c41d6b06d42172c87a337`,API 调用方式已就绪
- 百炼 API:`POST {AgentBailianHost}/api/v1/apps/{AppId}/completion`(Host 为业务空间专属端点,非默认 dashscope.aliyuncs.com),
  Bearer DASHSCOPE_API_KEY,prompt 入参,output.text 出参(流式可选)
- AI 收正式开通未完成(需企业支付宝扫码签约)——不阻塞 Phase 1

## 3. 架构

```
用户 ↔ 主站前端 /console/agent-chat(新页面,复用 playground 的 ai-elements 组件)
        ↓ POST /api/agent/chat (新增,登录态 + CriticalRateLimit)
new-api 后端 controller/service(保管 DASHSCOPE_API_KEY,运营设置项)
        ↓ DashScope HTTP API(不用 SDK,纯 HTTP 调用对齐 Go 服务端风格)
百炼智能体 cb7afba7...(大脑 + AI 支付体验版 MCP 工具)
```

## 4. 组件与数据流

### 后端(new-api,Go)
- `router/api-router.go`: `selfRoute` 或 `userRoute` 下注册 `POST /agent/chat`,
  挂 `middleware.CriticalRateLimit()`(对齐 /alipay/pay 防护级别)
- `controller/agent_chat.go`: 参数校验(prompt 非空、长度上限、session_id 可选透传
  conversation_id 维持多轮),调用 service,返回 output.text
- `service/dashscope_agent.go`: 纯 HTTP 调用百炼 completion 接口;API Key/App ID
  从运营设置读取(对齐支付宝密钥管理范式:改配置需重启容器生效)
- JSON 一律走 common/json.go 包装(技术红线)

### 前端(web/default/src)
- `features/agent-chat/` 新模块:
  - 页面骨架复用 `playground` 的 Conversation/Message/ai-elements 组件
  - 输入框复用 `playground-input` 的交互范式(简化:无分支/编辑)
  - **支付卡片识别**:检测智能体回复中的支付链接(URL 正则),渲染为卡片:
    金额 + 标题 + 协议勾选(《用户协议》《隐私政策》,复用 wallet 的勾选组件逻辑,
    勾选前按钮置灰、每次打开重置)+ 支付按钮
  - 路由注册到 console 区域,sidebar 可见性走现有模块开关体系(playground 同款)
- i18n 同步 en/ja/fr/ru/vi/zh 六语言

### 多轮会话
- Phase 1 用 DashScope 的 conversation_id 维持上下文(session_id 由前端生成/保存
  于 localStorage,key 对齐 playground 的 storage 范式)

## 5. 错误处理
- 后端:百炼超时/非 200 → 统一 `{"message":"error","data":"智能体服务暂不可用"}`;
  API Key 未配置 → 同支付宝 handler 的「当前管理员未配置支付信息」文案范式
- 前端:消息流中渲染错误气泡(复用 MessageError 组件),可重试

## 6. 测试计划
- 后端单测:controller 参数校验、service 的 HTTP mock(对齐 topup_alipay_test.go 范式)
- 手工验收:0.01 元体验版全流程——「我要充值 1 元」→ 支付卡片(勾选前按钮置灰)
  → 点击支付 → 支付宝测试收银台 → 支付宝扣款 0.01 → 智能体查询工具确认
- 验证记录落 `docs/records/`(一问题一 md)

## 7. 边界与非目标(Phase 1)
- 不做记账桥(支付成功不加钱包额度,体验版资金本来不进生产账户)
- 不做流式输出(Phase 1 非流式够用,流式后置)
- 不动现有 wallet/subscriptions 页面与支付链路
- 不接 hermes-workspace,不碰百炼 AI 收正式签约
- 智能体提示词由用户在百炼控制台维护,本任务只负责管道

## 8. Phase 2 预留(不实施)
- AI 收正式开通(企业支付宝扫码)→ 生产密钥替换
- 记账桥:后端轮询智能体「查询支付」工具结果 → 到账后加钱包额度;userId 透传
  (DashScope 支持 business_id/userId 参数)实现订单归属
