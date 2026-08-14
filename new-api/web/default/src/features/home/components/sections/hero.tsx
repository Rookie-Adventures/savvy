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
import { HeroEmberCanvas } from './hero-ember-canvas'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

export function Hero(props: HeroProps) {
  const { t } = useTranslation()

  return (
    <section className='relative overflow-hidden px-6 pt-28 pb-24 md:pt-40 md:pb-28 lg:pt-48 lg:pb-32'>
      {/* ponytail: SPECTACLE 2→4 — a slow-drifting Canvas 2D ember-cloud
        texture in the existing --warning accent, sitting under the grain
        and vignette so the substrate order stays engine→grain→vignette→
        content. Freezes to a still frame under prefers-reduced-motion
        (handled inside the component itself). */}
      <HeroEmberCanvas />

      {/* ponytail: editorial substrate — grain + vignette as local decor,
        injected nowhere else; the global theme tokens carry all neutrals.
        No blob, no grid, no gradient — the headline earns the whitespace. */}
      <div
        aria-hidden
        className='pointer-events-none fixed inset-0 z-0 opacity-[0.03] dark:opacity-[0.04]'
        style={{
          backgroundImage:
            "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='200' height='200'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.75' numOctaves='4'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E\")",
        }}
      />
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0 -z-10 dark:block hidden'
        style={{
          background:
            'radial-gradient(ellipse 90% 70% at 50% 38%, transparent 50%, oklch(0.18 0 0 / 0.9) 100%)',
        }}
      />

      <div className='relative z-10 mx-auto flex max-w-4xl flex-col items-start'>
        {/* ponytail: eyebrow stays cut per the earlier trim — the mono
          capability line below was restored on user request (2026-08-12);
          only the eyebrow tag and second paragraph remain cut. */}
        <h1
          className='landing-animate-fade-up text-foreground text-[clamp(2.5rem,6vw,4.75rem)] font-medium leading-[1.02] tracking-[-0.035em] opacity-0 [text-wrap:balance]'
          style={{ fontFamily: 'var(--font-serif)' }}
        >
          {t('Your work agent.')}
          <br />
          <span className='text-warning'>
            {t('Open one, ship today.')}
          </span>
        </h1>

        <p
          className='landing-animate-fade-up text-muted-foreground-soft mt-8 max-w-xl text-base leading-relaxed opacity-0 md:text-lg [text-wrap:pretty]'
          style={{ animationDelay: '80ms' }}
        >
          {t(
            'One workspace for sellers, writers, designers and everyone who ships.'
          )}
        </p>

        {/* ponytail: restored capability sub-line (2026-08-12) — same
          i18n key the pre-redesign hero used; zh.json already carries the
          "多款主流 AI 能力已接入 · 无需配置 · 注册即开工" copy the user
          asked back for, so no locale-file edit was needed here. */}
        <p
          className='landing-animate-fade-up text-muted-foreground-soft mt-3 max-w-xl font-mono text-[13px] leading-relaxed opacity-0 [text-wrap:pretty]'
          style={{ animationDelay: '120ms' }}
        >
          {t('Forty-plus models, already wired in.')}
        </p>

        <div
          className='landing-animate-fade-up mt-12 flex flex-wrap items-center gap-3 opacity-0'
          style={{ animationDelay: '200ms' }}
        >
          {props.isAuthenticated ? (
            <>
              <Button
                className='group h-11 rounded-lg px-5 text-sm font-medium'
                render={<Link to='/dashboard' />}
              >
                {t('Go to Workspace')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
            </>
          ) : (
            <>
              <Button
                className='group h-11 rounded-lg px-5 text-sm font-medium'
                render={<Link to='/sign-up' />}
              >
                {t('Get Started')}
                <ArrowRight className='ml-1.5 size-4 transition-transform duration-200 group-hover:translate-x-0.5' />
              </Button>
              <Button
                variant='outline'
                className='border-border/50 hover:border-border hover:bg-muted/50 h-11 rounded-lg px-5 text-sm font-medium'
                render={<Link to='/pricing' />}
              >
                {t('See Pricing')}
              </Button>
            </>
          )}
        </div>
      </div>
    </section>
  )
}
