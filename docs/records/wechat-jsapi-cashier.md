# 微信内 JSAPI 收银台落地记录

## 现象/目标
微信内置浏览器打开 scheng.net 钱包/订阅页,点「微信支付」希望能直接拉起微信支付(JSAPI);
微信外保持现有 Native 二维码不变;回调零改动复用现有 notify。

## 方案
- 抽共享 client 装配 buildWechatCoreClient(Native/JSAPI 共用,公钥/平台证书两模式唯一来源),
  新增 JSAPI 单例 GetWechatJsapiClient;统一 verifier getWechatVerifier 供 notify 复用
  → 从根上满足「请求/回调验签同模式」铁律(2026-09-12 事故)。
- snsapi_base 静默授权拿 openid 存 session(含 state 防 CSRF + isSafeLocalRedirect 防 open-redirect)。
- JSAPI 下单端点用 SDK PrepayWithRequestPayment 一步出调起参数(不手写 paySign);
  微信内前端 WeixinJSBridge.invoke('getBrandWCPayRequest') 调起,微信外维持二维码。
- 后台 AppSecret 经 OptionMap 持久化 + UI 字段;微信 topup 回调补写 CompleteTime。

## 改动清单
- controller/subscription_payment_wechat.go(抽 buildWechatCoreClient + JSAPI 单例 + getWechatVerifier)
- controller/wechat_notify.go(verifier 改调 getWechatVerifier)
- service/wechat_oauth.go + controller/wechat_jsapi_oauth.go(OAuth 桥 + 防御)
- controller/topup_wechat.go + subscription_payment_wechat.go(JSAPI 下单端点)
- 前端 wallet lib/api/hooks + subscriptions dialog(微信内分支)
- model/option.go(WechatAppSecret 持久化)+ UI 字段
- controller/topup_wechat.go(CompleteTime 补写)

## 验证
- 后端 go test ./setting/... ./service/... ./controller/... ./model/... 全绿
  (service 包存量 2 个 channel-affinity 失败项系更早合并引入,与本分支无关——本分支在
  service/ 仅改 wechat_oauth.go 及其测试);go build ./... 通过。
- 前端 npm run build 通过;tsc --noEmit 无新增错误。
- (生产灰度由用户在后台填真实 AppId/AppSecret/密钥后验收;本任务不部署、不动生产 DB。)

## 限制
- 微信内支付结果以回调落地为准;前端仅提示「已在微信内发起」。
- openid 依赖一次静默授权跳转(微信带回 ?wechat_jsapi=1 后再次发起支付)。
- AppSecret/密钥真实值由用户后台配置,本任务不触碰平台配置真实值。

## 尾巴
- 后续可加:JSAPI 下单失败主动查单对账(对齐 2026-09-12 事故尾巴)。
- 小程序支付可复用同一 buildWechatCoreClient + verifier 选择逻辑。
