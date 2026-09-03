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
import { useState } from 'react'
import { ClaimCard } from './claim-card'
import { readClaims, type ClaimRecord } from '../lib/claim-storage'

// 登录/注册回跳后聊天消息态已丢,未认领单由这里接力(sessionStorage 恢复)。
// widget 浮窗与 /agent 独立页共用;挂载时读取,认领完成后下次挂载自动消失。
export function ClaimBanner() {
  const [claims] = useState<ClaimRecord[]>(() =>
    readClaims().filter((r) => !r.done)
  )
  if (claims.length === 0) return null
  return (
    <div className='px-1'>
      {claims.map((r) => (
        <ClaimCard key={r.outTradeNo} outTradeNo={r.outTradeNo} token={r.token} />
      ))}
    </div>
  )
}
