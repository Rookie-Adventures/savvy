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

interface TrustModelStabilityProps {
  className?: string
}

// ponytail: answers the two fears a buyer in this market brings — fake
// models (cheap swapped for premium) and unreliability (drops, losses).
// Deliberately does NOT count models (count invites the very suspicion it's
// trying to dispel); states the one guarantee that actually matters: the
// call goes to the upstream model you asked for, undisturbed. Stability is
// grounded in the free trial itself — "try it yourself for two hours" beats
// any "we are reliable" claim.
export function TrustModelStability(_props: TrustModelStabilityProps) {
  const { t } = useTranslation()

  return (
    <section className='border-border/40 border-t px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-5xl'>
        <div className='grid grid-cols-1 gap-16 md:grid-cols-2 md:gap-20'>
          <AnimateInView animation='fade-up'>
            <p className='text-muted-foreground/50 mb-5 font-mono text-[11px] tracking-[0.2em] uppercase'>
              {t('Fake models are common here')}
            </p>
            <h3
              className='text-foreground text-xl leading-snug font-medium md:text-2xl'
              style={{ fontFamily: 'var(--font-serif)' }}
            >
              {t('The model you ask for is the model you get.')}
            </h3>
            <p className='text-muted-foreground/80 mt-5 text-base leading-relaxed [text-wrap:pretty]'>
              {t(
                'Requests reach the upstream provider untouched. No rerouting to a cheaper tier, no silent downgrades. If you asked for the flagship, you run the flagship.'
              )}
            </p>
          </AnimateInView>

          <AnimateInView animation='fade-up' delay={80}>
            <p className='text-muted-foreground/50 mb-5 font-mono text-[11px] tracking-[0.2em] uppercase'>
              {t('Services drop. Yours does not have to.')}
            </p>
            <h3
              className='text-foreground text-xl leading-snug font-medium md:text-2xl'
              style={{ fontFamily: 'var(--font-serif)' }}
            >
              {t('Hosted, kept, and yours to verify first.')}
            </h3>
            <p className='text-muted-foreground/80 mt-5 text-base leading-relaxed [text-wrap:pretty]'>
              {t(
                'We keep the lights on; your machine does not stay awake for it. Try it through your real workload before you pay a yuan.'
              )}
            </p>
          </AnimateInView>
        </div>
      </div>
    </section>
  )
}
