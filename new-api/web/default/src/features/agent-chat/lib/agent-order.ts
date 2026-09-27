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

// 从 MCP 支付链接解析订单号与申报金额。链接形如
// https://openapi.alipay.com/gateway.do?...&biz_content=%7B%22out_trade_no%22...%7D
// 解析失败返回 null → 不显示认领卡片,退化为普通支付卡片(客服兜底)。
export function parseAgentOrder(
  link: string
): { outTradeNo: string; totalAmount: number } | null {
  try {
    const biz = new URL(link).searchParams.get('biz_content')
    if (!biz) return null
    const obj = JSON.parse(biz) as Record<string, unknown>
    const outTradeNo =
      typeof obj.out_trade_no === 'string' ? obj.out_trade_no : ''
    const totalAmount = Number(obj.total_amount)
    if (!outTradeNo || !Number.isFinite(totalAmount) || totalAmount <= 0)
      return null
    return { outTradeNo, totalAmount }
  } catch {
    return null
  }
}

// 微信智能体单在下单时就已在服务端落库并预发 claim_token(随回复文本带回),
// 前端只需从文本还原三件套,不走 alipay 那套 register 登记换 token 流程。
export function parseWechatAgentOrder(text: string): {
  outTradeNo: string
  claimToken: string
  claimUrl: string
} | null {
  const outTradeNo = text.match(/WXAGT\d{14}[A-Za-z0-9]{10}/)?.[0] ?? ''
  const claimUrl =
    Array.from(
      text.matchAll(/https?:\/\/[^\s<>"'`)\]]+/g),
      (m) => m[0]
    ).find((u) => u.includes('claim_token=')) ?? ''
  const claimToken =
    claimUrl.match(/claim_token=([a-f0-9]{32})/)?.[1] ??
    text.match(/\b[a-f0-9]{32}\b/)?.[0] ??
    ''
  if (!outTradeNo || !claimToken) return null
  return { outTradeNo, claimToken, claimUrl }
}
