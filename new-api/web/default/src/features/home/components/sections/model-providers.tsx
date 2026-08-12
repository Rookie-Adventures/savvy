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
import {
  Claude,
  Cohere,
  DeepSeek,
  Gemini,
  Grok,
  Minimax,
  Mistral,
  Moonshot,
  OpenAI,
  Perplexity,
  Qwen,
  Zhipu,
} from '@lobehub/icons'
import { AnimateInView } from '@/components/animate-in-view'

interface ModelProvidersProps {
  className?: string
}

// ponytail: bare marks only — no card, no background chip, no border.
// Doubao/ERNIE dropped (2026-08-12) — Volcengine's/Baidu's generic
// corporate marks don't read as those specific products, looked wrong
// rather than just plain. Every name below has a live adapter in
// relay/channel/ (claude, openai, gemini, ali, zhipu, deepseek, moonshot,
// minimax, xai, mistral, cohere, perplexity).
const PROVIDERS = [
  { Icon: Claude, name: 'Claude' },
  { Icon: OpenAI, name: 'GPT' },
  { Icon: Gemini, name: 'Gemini' },
  { Icon: Qwen, name: 'Qwen' },
  { Icon: Zhipu, name: 'GLM' },
  { Icon: DeepSeek, name: 'DeepSeek' },
  { Icon: Moonshot, name: 'Kimi' },
  { Icon: Minimax, name: 'MiniMax' },
  { Icon: Grok, name: 'Grok' },
  { Icon: Mistral, name: 'Mistral' },
  { Icon: Cohere, name: 'Cohere' },
  { Icon: Perplexity, name: 'Perplexity' },
]

export function ModelProviders(_props: ModelProvidersProps) {
  return (
    <section className='px-6 py-14 md:py-16'>
      <div className='mx-auto max-w-5xl'>
        <AnimateInView animation='fade-up'>
          <div className='grid grid-cols-3 gap-x-6 gap-y-8 sm:grid-cols-4 md:grid-cols-6'>
            {PROVIDERS.map((provider) => (
              <div
                key={provider.name}
                className='text-muted-foreground/70 hover:text-foreground flex flex-col items-center gap-2.5 transition-colors'
              >
                <provider.Icon aria-hidden='true' className='size-8' />
                <span className='text-sm font-medium'>{provider.name}</span>
              </div>
            ))}
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
