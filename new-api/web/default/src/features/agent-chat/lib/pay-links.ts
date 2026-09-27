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

/**
 * Extract payment links from agent reply text.
 * ponytail: 认三类——alipay 网关 URL、weixin://wxpay(Native code_url)、
 * payapp.weixin.qq.com(微信AI支付绑定/授权页)。漏判无害(退化为普通文本),误判会弹支付卡
 */
export function extractPayLinks(text: string): string[] {
  const https = [...text.matchAll(URL_RE)]
    .map((m) => m[0])
    .filter((u) => /alipay/i.test(u) || u.includes('payapp.weixin.qq.com'))
  const wechat = [...text.matchAll(WEIXIN_RE)].map((m) => m[0])
  return [...https, ...wechat]
}

/**
 * Strip payment links from text for display (card replaces the raw URL).
 * 剥掉链接后残留的空行/孤零标点一并收敛,避免气泡里剩一坨空白
 * 微信单的 claim_url 一并剥掉——卡片里有可点版本,裸文本在气泡里点不动
 */
export function stripPayLinks(text: string): string {
  const stripped = text
    .replace(URL_RE, (u) =>
      /alipay/i.test(u) || u.includes('claim_token=') || u.includes('payapp.weixin.qq.com') ? '' : u
    )
    .replace(WEIXIN_RE, '')
  return stripped
    .replaceAll('``', '') // 剥掉反引号包裹的链接后残留的空对
    .replace(/\[[^\]]*\]\(\s*\)/g, '') // markdown 链接被剥成 [text]() 后清掉
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .join('\n')
    .trim()
}
