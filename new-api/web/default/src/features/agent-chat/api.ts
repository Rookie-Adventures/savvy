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
import { api } from '@/lib/api'

export type AgentChatResponse = {
  message: string
  data: {
    text: string
    session_id: string
  }
}

/**
 * Send one chat turn to the Bailian agent (non-stream).
 * Mirrors wallet requestAlipayQRPayment's path + skipBusinessError pattern.
 */
export async function sendAgentMessage(
  prompt: string,
  sessionId: string
): Promise<AgentChatResponse> {
  const res = await api.post(
    '/api/user/agent/chat',
    { prompt, session_id: sessionId },
    { skipBusinessError: true } as Record<string, unknown>
  )
  return res.data
}

export type AgentTopUpResponse<T> = { message: string; data?: T }

/** 登记 MCP 订单换认领凭据(游客可用,登录态自动绑单)。 */
export async function registerAgentTopUp(outTradeNo: string) {
  const res = await api.post(
    '/api/user/agent/topup/register',
    { out_trade_no: outTradeNo },
    { skipBusinessError: true } as Record<string, unknown>
  )
  return res.data as AgentTopUpResponse<{ claim_token: string }>
}

/** 凭 claim_token 查订单状态;后端 pending 超 10s 会顺手向支付宝查单兜底。 */
export async function getAgentTopUpStatus(claimToken: string) {
  const res = await api.get(
    `/api/user/agent/topup/status?claim_token=${encodeURIComponent(claimToken)}`,
    { skipBusinessError: true } as Record<string, unknown>
  )
  return res.data as AgentTopUpResponse<{
    status: string
    money: number
    claimed: boolean
  }>
}

/** 登录态下凭 claim_token 认领已支付订单并入账(幂等)。 */
export async function claimAgentTopUp(claimToken: string) {
  const res = await api.post(
    '/api/user/agent/topup/claim',
    { claim_token: claimToken },
    { skipBusinessError: true } as Record<string, unknown>
  )
  return res.data as AgentTopUpResponse<{ amount: number }>
}
