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
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { SendHorizonal } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import { Message, MessageContent } from '@/components/ai-elements/message'
import { Loader } from '@/components/ai-elements/loader'
import { PaymentCard } from './components/payment-card'
import { extractPayLinks, stripPayLinks } from './lib/pay-links'
import { sendAgentMessage } from './api'

type ChatMessage = {
  role: 'user' | 'assistant'
  content: string
}

// 百炼云端会话 id,存 localStorage 续多轮(对齐 playground storage 范式)
const SESSION_KEY = 'agent_chat_session_id'

export function AgentChat() {
  const { t } = useTranslation()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const sessionIdRef = useRef<string>(
    typeof window === 'undefined'
      ? ''
      : (window.localStorage.getItem(SESSION_KEY) ?? '')
  )

  const send = async () => {
    const prompt = input.trim()
    if (!prompt || loading) return
    setInput('')
    setMessages((prev) => [...prev, { role: 'user', content: prompt }])
    setLoading(true)
    try {
      const res = await sendAgentMessage(prompt, sessionIdRef.current)
      if (res.message === 'success' && res.data) {
        if (res.data.session_id) {
          sessionIdRef.current = res.data.session_id
          window.localStorage.setItem(SESSION_KEY, res.data.session_id)
        }
        setMessages((prev) => [
          ...prev,
          { role: 'assistant', content: res.data.text },
        ])
      } else {
        setMessages((prev) => [
          ...prev,
          { role: 'assistant', content: t('Agent service is unavailable') },
        ])
      }
    } catch {
      setMessages((prev) => [
        ...prev,
        { role: 'assistant', content: t('Agent service is unavailable') },
      ])
    } finally {
      setLoading(false)
    }
  }

  // 布局对齐 playground:根容器锁高度,聊天区滚动,输入框固定在滚动区外
  return (
    <div className='relative flex size-full flex-col overflow-hidden'>
      <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
        <Conversation className='flex-1'>
          <ConversationContent className='p-0'>
            <div className='mx-auto w-full max-w-3xl px-4 py-6'>
              {messages.map((m, i) => {
                const payLinks =
                  m.role === 'assistant' ? extractPayLinks(m.content) : []
                const displayText =
                  payLinks.length > 0 ? stripPayLinks(m.content) : m.content
                return (
                  <Message
                    key={i}
                    from={m.role}
                    className='group flex-row-reverse'
                  >
                    <MessageContent>
                      {displayText}
                      {payLinks.map((link) => (
                        <PaymentCard key={link} link={link} />
                      ))}
                    </MessageContent>
                  </Message>
                )
              })}
              {loading && (
                <Message from='assistant'>
                  <MessageContent>
                    <div className='flex items-center gap-2 py-2'>
                      <Loader />
                      <span className='text-muted-foreground text-sm'>
                        {t('AI assistant is thinking...')}
                      </span>
                    </div>
                  </MessageContent>
                </Message>
              )}
            </div>
          </ConversationContent>
          <ConversationScrollButton />
        </Conversation>
      </div>

      <div className='mx-auto w-full max-w-3xl px-4 pb-4'>
        <div className='bg-background flex items-end gap-2 rounded-xl border p-2 shadow-sm'>
          <Textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.nativeEvent.isComposing) return
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void send()
              }
            }}
            placeholder={t('Type a message...')}
            rows={2}
            className='max-h-40 min-h-10 flex-1 resize-none border-0 shadow-none focus-visible:ring-0 dark:bg-transparent'
          />
          <Button
            size='icon'
            className='mb-0.5 shrink-0 rounded-lg'
            onClick={() => void send()}
            disabled={loading || !input.trim()}
          >
            <SendHorizonal className='h-4 w-4' />
          </Button>
        </div>
      </div>
    </div>
  )
}
