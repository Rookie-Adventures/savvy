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
// 反引号必须排除:智能体常以 `url` 形式贴链接,脏尾字符会进二维码值/跳转 href
const URL_RE = /https?:\/\/[^\s<>"'`)\]]+/g
// 微信 Native 下单返回的 code_url,浏览器打不开,只能渲染成二维码扫码
const WEIXIN_RE = /weixin:\/\/wxpay\/[^\s<>"'`)\]]+/g
// 微信 AI 付(X402 路径A)授权页。本宿主不提供该路径,出现即幻觉,不可点也不该点
const PAYAPP_RE = /https?:\/\/payapp\.weixin\.qq\.com[^\s<>"'`)\]]*/g

/**
 * Extract payment links from agent reply text.
 * ponytail: 认两类——alipay 网关 URL、weixin://wxpay(Native code_url)。
 * payapp 曾在此列(路径A),摘除后模型仍会照抄历史里的授权链接,故改为剥掉(见 stripAgentAuthLinks)
 */
export function extractPayLinks(text: string): string[] {
  const https = [...text.matchAll(URL_RE)]
    .map((m) => m[0])
    .filter((u) => /alipay/i.test(u))
  const wechat = [...text.matchAll(WEIXIN_RE)].map((m) => m[0])
  return [...https, ...wechat]
}

/**
 * 剥掉幻觉出来的 AI 付授权链接(连同被剥空的 markdown 链接),不留可点入口。
 * 与 stripPayLinks 分开:这条对所有消息无条件生效,那条只在识别到支付卡时跑(会收敛空行,
 * 不该动普通正文的缩进与换行)。
 */
export function stripAgentAuthLinks(text: string): string {
  return text
    .replaceAll(PAYAPP_RE, '')
    .replaceAll(/\[[^\]]*\]\(\s*\)/g, '')
}

/**
 * Strip payment links from text for display (card replaces the raw URL).
 * 剥掉链接后残留的空行/孤零标点一并收敛,避免气泡里剩一坨空白
 * 微信单的 claim_url 一并剥掉——卡片里有可点版本,裸文本在气泡里点不动
 */
export function stripPayLinks(text: string): string {
  const stripped = text
    .replace(URL_RE, (u) =>
      /alipay/i.test(u) || u.includes('claim_token=') ? '' : u
    )
    .replace(WEIXIN_RE, '')
    .replace(PAYAPP_RE, '')
  return stripped
    .replaceAll('``', '') // 剥掉反引号包裹的链接后残留的空对
    .replace(/\[[^\]]*\]\(\s*\)/g, '') // markdown 链接被剥成 [text]() 后清掉
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .join('\n')
    .trim()
}
