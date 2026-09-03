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
import { isSidebarModuleEnabled } from '@/lib/nav-modules'
import { AgentChat } from './index'
import { ClaimBanner } from './components/claim-banner'

// 全站右下角悬浮智能体入口。显隐复用原 /agent-chat 的模块开关(chat.agent_chat),
// 语义从"侧边栏模块"变为"widget 显隐",配置键不动,免迁移。
// 关闭按钮在聊天头部栏(ChatHeader),浮窗打开时每次挂载 ClaimBanner 恢复未认领单。
export function AgentWidget() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  if (!isSidebarModuleEnabled('chat', 'agent_chat')) return null

  return (
    <>
      <Button
        size='icon'
        className='fixed right-4 bottom-4 z-50 h-12 w-12 rounded-full shadow-lg'
        onClick={() => setOpen((v) => !v)}
        aria-label={t('AI assistant')}
      >
        <Bot className='h-5 w-5' />
      </Button>
      {open && (
        <div className='bg-background fixed right-4 bottom-20 z-50 flex h-[min(640px,80vh)] w-[min(420px,calc(100vw-2rem))] flex-col overflow-hidden rounded-xl border shadow-xl'>
          <div className='overflow-y-auto'>
            <ClaimBanner />
          </div>
          <div className='min-h-0 flex-1'>
            <AgentChat onClose={() => setOpen(false)} />
          </div>
        </div>
      )}
    </>
  )
}
