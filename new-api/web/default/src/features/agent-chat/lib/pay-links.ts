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
const URL_RE = /https?:\/\/[^\s<>"')\]]+/g

/**
 * Extract payment links from agent reply text.
 * ponytail: 只认包含 alipay 的 URL,漏判无害(退化为普通文本),误判会弹支付卡
 */
export function extractPayLinks(text: string): string[] {
  return Array.from(text.matchAll(URL_RE))
    .map((m) => m[0])
    .filter((u) => /alipay/i.test(u))
}

/**
 * Strip payment links from text for display (card replaces the raw URL).
 * 剥掉链接后残留的空行/孤零标点一并收敛,避免气泡里剩一坨空白
 */
export function stripPayLinks(text: string): string {
  const stripped = text.replace(URL_RE, (u) =>
    /alipay/i.test(u) ? '' : u
  )
  return stripped
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .join('\n')
    .trim()
}
