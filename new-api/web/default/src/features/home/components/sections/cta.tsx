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
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AnimateInView } from '@/components/animate-in-view'

interface CTAProps {
  className?: string
}

export function CTA(props: CTAProps) {
  const { t } = useTranslation()

  if (props.isAuthenticated) {
    return null
  }

  // ponytail: old CTA layered a dual radial-gradient mesh background plus a
  // blue→violet→purple gradient headline, and pitched "deploy your own
  // gateway" — the exact AI-purple-glow + API-gateway tells we're clearing.
  // Replaced with whitespace, a single serif line, and one hot-accent button.
  return (
    <section className='px-6 py-32 md:py-48'>
      <div className='mx-auto max-w-3xl text-center'>
        <AnimateInView animation='fade-up'>
          <h2
            className='text-foreground text-[clamp(2rem,5vw,3.5rem)] font-medium leading-[1.05] tracking-[-0.035em] [text-wrap:balance]'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('Open your work agent.')}
          </h2>
          <p className='text-muted-foreground/70 mx-auto mt-6 max-w-md text-base leading-relaxed [text-wrap:pretty]'>
            {t(
              'Two hours free, every start. No setup, no keys to gather, no card.'
            )}
          </p>
          <div className='mt-10 flex items-center justify-center'>
            <Button
              className='group h-12 rounded-lg px-7 text-base font-medium'
              render={<Link to='/sign-up' />}
            >
              {t('Get Started')}
              <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
            </Button>
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
