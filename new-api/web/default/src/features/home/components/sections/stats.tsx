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

interface StatsProps {
  className?: string
}

// ponytail: the old Stats counted fake-precise numbers (50+, 100+, …) with no
// source — that's the #1 anti-cheap tell. Replaced with two real, sourced
// facts the product actually ships: a free trial window and data retention.
// No Counter, no animation, no number chase. Editorial whitespace carries it.
export function Stats(_props: StatsProps) {
  const { t } = useTranslation()

  return (
    <section className='px-6 py-20 md:py-24'>
      <div className='text-muted-foreground/60 mx-auto max-w-3xl'>
        <AnimateInView animation='fade-up' className='border-border/40 border-t'>
          <p
            className='text-foreground/90 -mt-3 bg-background inline-block pr-4 text-xl leading-relaxed font-medium [text-wrap:pretty] md:text-2xl'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('Free for two hours, every start.')}
            <br />
            <span className='text-muted-foreground/60'>
              {t(
                'Open it for a listing, a draft, a design. Close it, and your work is kept.'
              )}
            </span>
          </p>
        </AnimateInView>
      </div>
    </section>
  )
}
