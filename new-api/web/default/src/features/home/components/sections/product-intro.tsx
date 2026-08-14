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

interface ProductIntroProps {
  className?: string
}

// ponytail: old ProductIntro was a 3-up icon card grid (Cloud / Server /
// Database) — the canonical "identical card grid" anti-cheap tell, and it
// buried the workspace story under generic infra icons. Replaced with two
// editorial columns: a serif proposition on the left, plain support on the
// right. No icons, no cards. One layout family, used once.
export function ProductIntro(_props: ProductIntroProps) {
  const { t } = useTranslation()

  return (
    <section className='px-6 py-24 md:py-32'>
      <div className='mx-auto grid max-w-5xl grid-cols-1 gap-12 md:grid-cols-12 md:gap-16'>
        <AnimateInView
          animation='fade-up'
          className='md:col-span-7'
        >
          <h2
            className='text-foreground text-[clamp(1.75rem,3.5vw,2.75rem)] font-medium leading-[1.1] tracking-[-0.03em] [text-wrap:balance]'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('An agent that turns up ready, not one you set up.')}
          </h2>
        </AnimateInView>

        <AnimateInView
          animation='fade-up'
          delay={100}
          className='text-muted-foreground/80 md:col-span-5 md:pt-2'
        >
          <p className='text-base leading-relaxed [text-wrap:pretty]'>
            {t(
              'No servers, no environments, no keys to fetch yourself. Whatever you ship, a product listing, a script, a design pass: open the agent and the work begins. Models already wired in, files where you left them.'
            )}
          </p>
          <p className='mt-4 text-sm leading-relaxed [text-wrap:pretty]'>
            <span className='text-muted-foreground/50'>
              {t(
                'Re-open it from any browser. Two hours free per start, your data kept across sleeps.'
              )}
            </span>
          </p>
        </AnimateInView>
      </div>
    </section>
  )
}
