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
import { z } from 'zod'
import { createFileRoute, redirect, useSearch } from '@tanstack/react-router'
import { PublicLayout } from '@/components/layout'
import { isSidebarModuleEnabled } from '@/lib/nav-modules'
import { AgentChat } from '@/features/agent-chat'
import { ClaimBanner } from '@/features/agent-chat/components/claim-banner'

// 跨端认领入口: 智能体把 /agent?claim_token=<token> 发给用户,点开即挂载认领卡片。
// out_trade_no 只作展示/去重键,可缺省。非法值由 zod 吃掉(退化成无参数),不报错——
// 分享链路里多带脏参数不该把公开页打挂。
const agentSearchSchema = z.object({
  claim_token: z.string().optional().catch(undefined),
  out_trade_no: z.string().optional().catch(undefined),
})

// 智能体独立页: 公开路由(无登录墙),供智易收签约"智能体访问地址"与分享直达。
// 与全站悬浮 widget 同源组件,显隐同用 chat.agent_chat 模块开关。
export const Route = createFileRoute('/agent')({
  beforeLoad: () => {
    if (!isSidebarModuleEnabled('chat', 'agent_chat')) {
      throw redirect({ to: '/' })
    }
  },
  validateSearch: agentSearchSchema,
  component: AgentPage,
})

function AgentPage() {
  const { claim_token: claimToken, out_trade_no: outTradeNo } = useSearch({
    from: '/agent',
  })
  return (
    <PublicLayout showMainContainer={false}>
      {/* PublicHeader 是 fixed h-16(sm:h-20),用 mt 顶开而非 pt,高度相应扣减,底部留 2rem */}
      <div className='mx-auto mt-16 flex h-[calc(100dvh-6rem)] w-full max-w-3xl flex-col gap-2 px-4 pb-4 sm:mt-20 sm:h-[calc(100dvh-7rem)]'>
        <ClaimBanner claimToken={claimToken} outTradeNo={outTradeNo} />
        <div className='bg-background min-h-0 flex-1 overflow-hidden rounded-xl border shadow-sm'>
          <AgentChat />
        </div>
      </div>
    </PublicLayout>
  )
}
