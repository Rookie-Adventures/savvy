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
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'
import { CodeBlock, CodeBlockCopyButton } from '@/components/ai-elements/code-block'

interface ApiTerminalProps {
  className?: string
}

function getCurrentOrigin(): string {
  if (typeof window === 'undefined') return 'https://scheng.net'
  return window.location.origin
}

// ponytail: the endpoint, path and request shape are the real ones (same
// normalizeEndpoint()-produced URL the authenticated dashboard's own curl
// card uses). Only the key is a placeholder — masking a credential is not
// the "fake terminal" the cheapness blacklist bans; a fabricated command
// with an invented endpoint would be.
export function ApiTerminal(_props: ApiTerminalProps) {
  const { t } = useTranslation()
  const endpoint = `${getCurrentOrigin()}/v1/chat/completions`
  const command = [
    `curl ${endpoint} \\`,
    '  -H "Content-Type: application/json" \\',
    '  -H "Authorization: Bearer sk-your-api-key" \\',
    '  -d \'{"model":"claude-opus-4.8","messages":[{"role":"user","content":"..."}]}\'',
  ].join('\n')

  return (
    <section className='px-6 pt-16 pb-24 md:pt-20 md:pb-32'>
      <div className='mx-auto max-w-4xl'>
        <AnimateInView animation='fade-up'>
          <p className='text-muted-foreground-faint mb-5 text-center font-mono text-[11px] tracking-[0.2em] uppercase'>
            {t('No new SDK to learn')}
          </p>
          <h3
            className='text-foreground mb-3 text-center text-2xl leading-snug font-medium md:text-3xl'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('One endpoint. The OpenAI request shape you already know.')}
          </h3>
          <p className='text-muted-foreground-soft mb-10 text-center text-sm leading-relaxed'>
            {t(
              'We guarantee it: every model on this platform is a genuine, manufacturer-mapped model — never faked, downgraded, or swapped.'
            )}
          </p>

          <div className='border-border overflow-hidden border'>
            {/* ponytail: real macOS traffic-light hex, not a themed gray —
              this is window chrome, not brand-accent surface, so the site's
              single-accent lock doesn't apply here. Background left to
              CodeBlock's own bg-background (it forces that token on the
              nested <pre> via !important) so the dots bar and the syntax-
              highlighted code share one tone instead of fighting a
              specificity battle over two competing backgrounds. Sharp
              corners (no rounded-*) to match ChatPreview's card. */}
            <div className='bg-background flex items-center gap-2 px-5 py-4'>
              <span className='size-3 rounded-full bg-[#ff5f57]' />
              <span className='size-3 rounded-full bg-[#febc2e]' />
              <span className='size-3 rounded-full bg-[#28c840]' />
            </div>
            <CodeBlock
              code={command}
              language='bash'
              className='rounded-none border-none [&_pre]:px-7 [&_pre]:pt-8 [&_pre]:pb-4 [&_pre]:text-base [&_pre]:leading-loose'
            >
              <CodeBlockCopyButton />
            </CodeBlock>
            {/* ponytail: a static next-prompt line — purely decorative, kept
              outside the CodeBlock's `code` prop so the copy button still
              copies exactly the real curl command, nothing extra. Gives the
              window some room to breathe below the command instead of the
              pane ending right at the last line, closer to how a real
              terminal looks with the cursor waiting on the next line. */}
            <div className='bg-background text-muted-foreground-faint px-7 pb-20 font-mono text-base'>
              <span className='text-muted-foreground-soft'>$</span>{' '}
              <span className='animate-pulse'>▍</span>
            </div>
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
