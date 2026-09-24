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

// 跨端直达: 智能体卡片/邮件/短信带的链接形如 /agent?claim_token=xxx,落库后
// 后续一切复用 sessionStorage 链路(sign-in 回跳带 search、sign-up 回跳不带也能接上)。
// 幂等: 同 token 已存在时直接返回,否则会把已认领记录的 done 打回 false 让卡片复活。
export function mergeUrlClaim(token: string, outTradeNo?: string): void {
  const trimmed = token.trim()
  if (!trimmed) return
  if (readClaims().some((r) => r.token === trimmed)) return
  // 卡片只把 outTradeNo 当去重键与入库标识,不展示;链接没带就用 token 兜底
  saveClaim({ outTradeNo: outTradeNo?.trim() || trimmed, token: trimmed, done: false })
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
