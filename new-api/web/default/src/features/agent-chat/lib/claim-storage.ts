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
export type ClaimRecord = { outTradeNo: string; token: string; done: boolean }

const KEY = 'agent_topup_claims'

// 认领凭据只活在本标签页会话(sessionStorage):跨登录/注册跳转不丢,关页即弃,
// 丢弃后凭支付宝账单订单号走客服人工认领(spec §5.6 兜底)。
export function readClaims(): ClaimRecord[] {
  try {
    return JSON.parse(sessionStorage.getItem(KEY) ?? '[]') as ClaimRecord[]
  } catch {
    return []
  }
}

export function saveClaim(rec: ClaimRecord): void {
  const all = readClaims().filter((r) => r.outTradeNo !== rec.outTradeNo)
  all.push(rec)
  sessionStorage.setItem(KEY, JSON.stringify(all))
}

export function markClaimDone(outTradeNo: string): void {
  sessionStorage.setItem(
    KEY,
    JSON.stringify(
      readClaims().map((r) =>
        r.outTradeNo === outTradeNo ? { ...r, done: true } : r
      )
    )
  )
}
