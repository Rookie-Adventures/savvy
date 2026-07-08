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

export function HowItWorks() {
  const { t } = useTranslation()

  // ponytail: old steps were "config keys → connect API routes → monitor
  // usage" — a pure API-gateway onboarding, contradicting the workspace
  // narrative. Rewritten as the workspace journey (open → use → return),
  // with the one quiet API note inside step 2. Mono ordinal replaces the
  // circular numbered badge (anti-cheap: numbered markers as architecture).
  const steps = [
    {
      n: '01',
      title: t('Open'),
      desc: t(
        'Sign up and the agent is there, waiting. No environment to build, no channel to configure first.'
      ),
    },
    {
      n: '02',
      title: t('Work'),
      desc: t(
        'Use the models wired into the agent, or bring your own key. Every keystroke stays in your space.'
      ),
    },
    {
      n: '03',
      title: t('Return'),
      desc: t(
        'Close the tab and the agent sleeps after two hours free. Open it again anywhere and carry on.'
      ),
    },
  ]

  return (
    <section className='border-border/40 border-t px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-5xl'>
        <AnimateInView animation='fade-up' className='mb-16 max-w-xl md:mb-20'>
          <h2
            className='text-foreground text-[clamp(1.75rem,3.5vw,2.75rem)] font-medium leading-[1.1] tracking-[-0.03em] [text-wrap:balance]'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('From nothing to shipping,')}
            <br />
            {t('in one open.')}
          </h2>
        </AnimateInView>

        <div className='border-border/40 divide-border/40 divide-y'>
          {steps.map((s, i) => (
            <AnimateInView
              key={s.n}
              animation='fade-up'
              delay={i * 100}
              className='grid grid-cols-1 items-baseline gap-4 py-8 md:grid-cols-12 md:gap-8 md:py-10'
            >
              <span className='text-muted-foreground/50 font-mono text-sm tracking-[0.2em] tabular-nums md:col-span-2'>
                {s.n}
              </span>
              <h3
                className='text-foreground text-lg leading-snug font-medium md:col-span-3'
                style={{ fontFamily: 'var(--font-serif)' }}
              >
                {s.title}
              </h3>
              <p className='text-muted-foreground/80 text-base leading-relaxed md:col-span-7 [text-wrap:pretty]'>
                {s.desc}
              </p>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
