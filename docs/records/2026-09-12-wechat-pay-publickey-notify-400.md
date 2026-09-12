# 2026-09-12 微信支付成功不加余额 — 回调验签未走公钥模式连吃 400

## 症状

- 生产真实支付 1 元成功（用户扫码付款完成），点「支付完成」后余额不变。
- 本地出码正常（统一下单通），仅回调环节失败。

## 排查过程（逐层取证）

| 层 | 取证 | 结果 |
| --- | --- | --- |
| 下单 | 机B GIN 日志 `POST /api/user/wechat/pay` | 200、444ms（真发了网络请求，出码成功） |
| 回调到达 | GIN 日志 `POST /api/user/wechat/notify`（来源 121.51.30.x = 微信） | **到达**，但连续 4 次 **400**（微信按重试策略补发） |
| 拒绝点 | 读 `controller/wechat_notify.go` | 400 只可能来自 `decryptWxNativeNotify` 验签/解密失败 |
| 根因定位 | 读 `decryptWxNativeNotify` 的 verifier 装配 | 固定用 `downloader.MgrInstance().GetCertificateVisitor(mchID)`（平台证书自动下载器） |

## 根因

请求端（`GetWechatClient`）在 2024-10 后新商户号场景走**公钥模式**（`WithWechatPayPublicKeyAuthCipher`），但**回调验签端没跟着改**：仍用平台证书自动下载器。新商户号微信不再下发平台证书 → 下载器取不到微信证书 → 签名验证必败 → 400 → 微信重试仍 400 → 订单永远 pending、不加余额。

两端验签源不对称是本质；同类不对称还有上一轮的公钥格式问题（商户平台下发单行 base64 无 PEM 头尾，`utils.LoadPublicKey` 只认 PEM 块 → 客户端构建静默返 nil，前端误报「未配置」）。

## 改动

- `controller/wechat_notify.go`：`decryptWxNativeNotify` 的 verifier 与 `GetWechatClient` 同模式——配置了 `WechatPayPublicKeyId`+`WechatPayPublicKey` 时用 `verifiers.NewSHA256WithRSAPubkeyVerifier(keyID, *pub)`（公钥经 `normalizeWechatPublicKey` 归一化）；否则保留平台证书路径（老商户号）。
- 同文件 `handleWxNotify` 的 400 拒绝路径补 `logger.LogError`（含 trade_no 与底层 err）——钱路径不再静默。
- 配套（同日上轮）：`normalizeWechatPublicKey` + 四条静默失败路径日志 + `TestNormalizeWechatPublicKey` 单测。

## 验证

- 本地 `go build ./controller/` 0 错、单测 ok。
- 机B ff 拉取 `63544187a` → detach 构建 → `up -d`。
- 22:59:28 微信重试回调 → **200**；`top_ups` 订单 82（`WXUSR1NOnZN7231789224298`, money=1）`pending → success`，额度已加。
- 幂等复核：finalize 对非 pending 订单直接返 SUCCESS 不重复加钱，重试安全。

## 限制

- 回调到账依赖微信重试（24h 内递减频率）；本次修复上线后下一次重试即入账，无需人工补单。
- 未实现主动查单对账（reconcile）：若微信重试耗尽仍失败，需人工凭商户平台账单补单。属已知尾巴。

## 尾巴

- 建议加一个「pending 超 N 分钟主动查单」的对账任务（调微信查单接口核对后入账），摆脱对重试的单一依赖。
- 支付类双端（请求/回调）装配验签源时，必须同模式；后续接 JSAPI/小程序支付时复用本 verifier 选择逻辑，别复制平台证书路径。
