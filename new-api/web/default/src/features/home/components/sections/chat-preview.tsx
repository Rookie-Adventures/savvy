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
import { Link } from '@tanstack/react-router'
import { ArrowUp, Lock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'

interface ChatPreviewProps {
  className?: string
  isAuthenticated?: boolean
}

// ponytail: this is a real, honest UI shell (the actual Textarea/Button
// primitives, not div-mocked screenshots) locked in a disabled state for
// signed-out visitors — same pattern the mulerouter reference uses ("Sign in
// to start chatting"). It never fakes a live model response and never calls
// the real chat backend from an anonymous session, so there is no free
// inference / abuse surface here. The one-line example below is labeled
// illustrative, not a captured transcript.
//
// ponytail (2026-08-12): sharp corners + bg-background (not bg-card) so the
// card reads as part of the page, not a lighter panel dropped on top —
// user called the rounded/lighter version "割裂". Textarea/button are back
// to normal input scale — scaling them up with the container made a chat
// composer look like an oversized empty box instead of a text field.
export function ChatPreview(props: ChatPreviewProps) {
  const { t } = useTranslation()

  return (
    <section className='px-6 py-16 md:py-20'>
      <div className='mx-auto max-w-4xl'>
        <AnimateInView animation='fade-up'>
          <h3
            className='text-foreground mb-3 text-center text-xl leading-snug font-medium md:text-2xl'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('No complex setup — a digital worker you carry everywhere.')}
          </h3>
          <p className='text-muted-foreground-soft mx-auto mb-10 max-w-md text-center text-base leading-relaxed [text-wrap:pretty]'>
            {t('No payment limits')}
          </p>

          {/* ponytail: fill stays bg-background — the lighter panel was the
            "割裂" the user rejected. The edge carries the separation instead,
            at full --border rather than /50, which on white was a 1.11:1
            hairline the card effectively did not have. */}
          <div className='border-border bg-background overflow-hidden border'>
            <div className='border-border flex items-center justify-between border-b px-6 py-5'>
              <span className='text-muted-foreground-soft font-mono text-sm'>
                {t('Hermes Work Agent')}
              </span>
              <span className='text-muted-foreground-faint font-mono text-xs'>
                claude-opus-4.8
              </span>
            </div>

            <div className='flex min-h-[28rem] flex-col items-center justify-center gap-6 px-6 py-16'>
              <img
                src='/hermes-mascot.png'
                alt=''
                aria-hidden='true'
                className='h-32 w-auto max-w-full object-contain'
              />
              <p className='text-muted-foreground-soft flex items-center gap-1.5 text-sm'>
                <Lock className='size-3.5' aria-hidden='true' />
                {props.isAuthenticated
                  ? t('Open the real workspace to chat.')
                  : t('Sign in to start chatting.')}
              </p>
            </div>

            <div className='border-border flex items-end gap-2 border-t p-4'>
              <Textarea
                disabled
                rows={1}
                placeholder={t('Ask it to draft a listing, a script, a spec…')}
                className='min-h-10 resize-none border-none bg-transparent text-sm shadow-none focus-visible:ring-0'
              />
              <Button
                size='icon'
                className='size-9 shrink-0 rounded-lg'
                render={
                  <Link to={props.isAuthenticated ? '/dashboard' : '/sign-up'} />
                }
                aria-label={t('Get Started')}
              >
                <ArrowUp className='size-4' />
              </Button>
            </div>
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
