/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// 微信内置浏览器判定。JSAPI 支付只能在微信 webview 里调起，
// 环境由 UA 确定性地判出——不让大模型猜自己在哪（官方文档亦警告模型分支判断不保证准确）。
export function isWeChatBrowser(): boolean {
  return typeof navigator !== 'undefined' && /MicroMessenger/i.test(navigator.userAgent)
}

const OAUTH_TRIED_KEY = 'agent_wechat_jsapi_oauth_tried'

export function wechatJsapiOauthStartUrl(next: string): string {
  return `/api/user/wechat/jsapi/oauth/start?redirect=1&next=${encodeURIComponent(next)}`
}

// 进页面就静默跳一次服务号授权(snsapi_base 不弹授权框)，把 openid 存进服务端 session，
// 之后"在微信里付款"才有付款人身份。
//
// 只跳一次：sessionStorage 闩 + 回跳带 wechat_jsapi 参数即视为已尝试。少了这道闩，
// 授权被拒(公众号未配网页授权域名等)时会形成无限重定向。
export function ensureWechatJsapiOpenid(): void {
  if (typeof window === 'undefined' || !isWeChatBrowser()) return
  const params = new URLSearchParams(window.location.search)
  if (params.get('wechat_jsapi')) return
  if (sessionStorage.getItem(OAUTH_TRIED_KEY)) return
  sessionStorage.setItem(OAUTH_TRIED_KEY, '1')
  window.location.replace(
    wechatJsapiOauthStartUrl(`${window.location.pathname}${window.location.search}`)
  )
}

// 点"在微信里付款"时后端回 wechat_oauth_required(session 里没 openid)：
// 清掉闩再跳一次授权。闩是防无限重定向用的，这里是一次有明确契机的重试，不是循环。
export function retryWechatJsapiOauth(): void {
  sessionStorage.removeItem(OAUTH_TRIED_KEY)
  window.location.href = wechatJsapiOauthStartUrl(
    `${window.location.pathname}${window.location.search}`
  )
}
