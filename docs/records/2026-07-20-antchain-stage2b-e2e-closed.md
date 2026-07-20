# 蚂蚁链订单存证段2b 真链 E2E + 生产付款回调闭环

留痕日期: 2026-07-20
状态: **完工,生产付款真单已验上链**

## 背景

段2a(付款三步 insertOrder→completeOrder→logOrder)已真链 E2E 通(见 2026-07-18 record)。
段2b 加发货/履约第四步 deliverOrder,让链证合成一条完整单据(付款9+发货2=11字段),独立可验,供支付宝冻结申诉作硬证据。

决策反转详见 `2026-07-19-antchain-evidence-dropped-decision.md` — 否决路线B(支付宝官方履约链路),回自架链证方案,合约加 deliverOrder。

## 实施改动三件套

### 1. 合约 `docs/solidity/OrderEvidence.sol`
- struct `Order` 加 `deliveredAt` + `deliveryHash`
- 加 `deliverOrder(string tradeNo, string browserJson, string deliveredAt, string deliveryHash) public onlyOwner`
  - require 订单已存在(insertOrder 写过)
  - 写 struct 两字段 + emit LOG_STRING(browserJson)
- 加 getter `getDeliveredAt(string)→string` / `getDeliveryHash(string)→string`

ABI 四参 string 透传 — deliverOrder 签名定了,后续 browserJson 内容变不需再重发合约。

### 2. model `new-api/model/antchain_evidence.go`
`BuildDeliverEvidence(in SubmitOrderEvidenceInput, deliveredAt string) DeliverEvidenceInput`:

- 11 中文锁字段(全进 deliveryHash canonicalJSON): 交易号/用户ID/付款金额_分/套餐ID/支付渠道/付款时间/付款状态/业务类型/付款指纹/发货时间 + 发货指纹(锁前10,不含自身)
- "付款指纹" = 付款侧 dataHash 原串复刻(段2a英文算法锁值,跨条连贯链住付款8字段)
- "付款金额_分"(非"充值金额_分") — 避开支付宝"虚拟充值"洗钱类目敏感词
- 收款主体 = 工商执照全称 "郑州市管城回族区栗橙网络科技工作室(个体工商户)" — 2026-07-20 核正,旧写"粟城科技网络工作室"已废弃
- 收款主体 + 收款域名(scheng.net) = 不进 deliveryHash 锁面(写死值,自己填自己锁无验用价值)

### 3. service `new-api/service/antchain/evidence.go`
- `SubmitEvidence` 尾串第四步 `DeliverOrder(in)`,与付款三步同 goroutine 串行 fire-and-forget
- 导出 `DeliverOrder(in SubmitOrderEvidenceInput)` 便于 payment 回调独立调
- **成功路径 trace 日志**(本次新补): 每步 `common.SysLog("antchain evidence[tradeNo] stepN ... ok")`,完成打 ALL STEPS PASS。修前 fire-and-forget silent 成功无法判上链否(docker logs 零 antchain 行)。

### 注入点


- `controller/topup_alipay.go:133` (直连支付宝)
- `controller/topup.go:400` (易支付聚合)
两处都 `if model.SubmitOrderEvidenceFn != nil { go func() {...} }` fire-and-forget。
注册在 `service/hermes.go:593` `model.SubmitOrderEvidenceFn = antchain.SubmitEvidence`(init 无条件)。

## 真链 E2E 裸跑(开发期)
合约重发: 旧合约 `OrderEvidence` 无 deliverOrder → revert。新发合约 **savvy1**(链上注册名 ≠ 编译文件名,踩坑码表 120 根因),合约 ID `a5fb52f8e0c7d2b8bc9a296e92e13029f50515d394258a10ce8b9600b39b5621`。

`rtk proxy go test -run TestSubmitEvidence_E2E -tags=manual` 多笔全 PASS,tradeNo 如 E2E-1784489506,链上 browser 查 13 键齐全(11锁+收款主体+收款域名)。

## 生产付款回调闭环验证(2026-07-20)

本机 Docker new-api 镜像已重建带段2b 11 中文键代码,跑 localhost:3000。
cloudflared quick tunnel: `https://above-yellow-beach-assured.trycloudflare.com`。
后台根服务器地址改到该 tunnel 域名(不带尾斜杠/路径),new-api GetCallbackAddress 自动拼 notify_url。

20 元沙箱真单 `ALIPAYUSR2NOENFDxS1784560576` 付款成功:
- top_ups status=success,complete_time 2026-07-20 15:16:41 UTC
- 支付宝 notify POST /api/user/alipay/notify 200(真从支付宝 IP 119.42.228.171 打来)

链证上链直查(getters 取 resp.Data, 见新增 TestQueryRealTrade_E2E):
- getTradeNo → "ALIPAYUSR2NOENFDxS1784560576" (insertOrder 写过)
- getDeliveredAt → "2026-07-20T15:16:54Z" (deliverOrder 第四步上了)
- getDeliveryHash → "8ce282fd0dc06a34701026334de17d288357ca7ff7d83228e09bb10333e44187" (非空=发货指纹写进 struct)

三 getter 全有值 = 四步 fire-and-forget 真上链,fire-and-forget silent 成功(日志零 antchain 行属正常,改 trace 前无成功留痕)。

## 踩坑码表(累计)

- **120** 合约名错配(链上注册名 ≠ 编译文件名)— FIX: env `ANTCHAIN_CONTRACT_NAME=savvy1`
- **10200** gas 上限小 — FIX: 500000
- **211** 同 account 高频限流 — FIX: step 间 sleep 3s
- **REST URL** `https://rest.baas.alipay.com`(带点)— 误拼 `restbaas` 超时
- **rtk 过滤吞 -run** — FIX: `rtk proxy go test` 绕
- **root 账户**: createRootAccountIfNeed 是 dead code 无人调 — 临时 psql 直插 bcrypt("12345678") 建库 root 用户

## 尾巴

- **trace 日志已补但未编进运行容器** — 镜像需重编才让 docker logs 见 stepN PASS。下次打款前重编。
- **重试队列**(方案三提的)未建 — 取决策文档的 fire-and-forget 更省,失败仅 SysError,YAGNI 等真漏调再做。
- **重发合约**: savvy1 是发新合约实例,旧 OrderEvidence 弃用(段2a E2E 测试单留在旧合约,无生产数据)。
- 生产机B生效: 重编 new-api 镜像 + `ANTCHAIN_CONTRACT_NAME=savvy1` env 注入。

## 参考
- 设计: `docs/solidity/muban.md`(段2b 对话全程)
- 决策: `2026-07-19-antchain-evidence-dropped-decision.md`
- 段2a 留痕: `2026-07-18-antchain-evidence-logrus-and-key-deploy.md`
- 合约: `docs/solidity/OrderEvidence.sol`
- memory: `project-antchain-stage2b-plan.md` / `project-antchain-evidence-stage2-stuck.md`
