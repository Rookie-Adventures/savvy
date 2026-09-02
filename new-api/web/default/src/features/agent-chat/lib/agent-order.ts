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
