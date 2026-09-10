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
import { useTranslation } from 'react-i18next'
import { Bot } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { isSidebarModuleEnabled } from '@/lib/nav-modules'
import { AgentChat } from './index'
import { ClaimBanner } from './components/claim-banner'

// 全站右下角悬浮智能体入口。显隐复用原 /agent-chat 的模块开关(chat.agent_chat),
// 语义从"侧边栏模块"变为"widget 显隐",配置键不动,免迁移。
// 关闭按钮在聊天头部栏(ChatHeader),浮窗打开时每次挂载 ClaimBanner 恢复未认领单。
export function AgentWidget(props: { onOpenChange?: (open: boolean) => void }) {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [open, setOpen] = useState(false)
  if (!isSidebarModuleEnabled('chat', 'agent_chat')) return null
  // 右下角堆叠从角落往上:客服球(WeComContactFab)占 bottom-4,机器人球
  // 堆其上(bottom-20)。聊天面板 sm+ 展开在机器人球**左侧并排**(旁边),
  // 不再堆在球上方;移动端窄屏仍堆上方,避免聊天窗被压成细条。
  const wecomFabOn = Boolean(status?.wecom_kf_url)
  // 开关上报给根路由,客服球的二维码卡据此避让面板。
  const setOpenState = (v: boolean) => {
    setOpen(v)
    props.onOpenChange?.(v)
  }

  return (
    <>
      <Button
        size='icon'
        className={`fixed right-4 z-50 size-12 rounded-full shadow-lg ${wecomFabOn ? 'bottom-20' : 'bottom-4'}`}
        onClick={() => setOpenState(!open)}
        aria-label={t('AI assistant')}
      >
        <Bot className='h-5 w-5' />
      </Button>
      {open && (
        <div className={`bg-background fixed z-50 flex h-[min(640px,80vh)] flex-col overflow-hidden rounded-xl border shadow-xl max-sm:right-4 max-sm:w-[min(420px,calc(100vw-2rem))] sm:right-[4.75rem] sm:bottom-4 sm:w-[min(420px,calc(100vw-6.75rem))] ${wecomFabOn ? 'max-sm:bottom-36' : 'max-sm:bottom-20'}`}>
          <div className='overflow-y-auto'>
            <ClaimBanner />
          </div>
          <div className='min-h-0 flex-1'>
            <AgentChat onClose={() => setOpenState(false)} />
          </div>
        </div>
      )}
    </>
  )
}
