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

interface LocalVsCloudProps {
  className?: string
}

// ponytail: the resulting of the home page's worst hook — "Hermes is
// open-source, anyone can self-host, so why pay us?" Left as an honest
// two-column contrast rather than smear: local conditions (zero cost, but
// install + to configure + leaving the machine on + no UI) vs. cloud (install
// and use + UI client + models pre-wired + 2h free trial + data
// retention). настроno smear, no false advantages, three concrete rows each.
export function LocalVsCloud(_props: LocalVsCloudProps) {
  const { t } = useTranslation()

  const rows = [
    {
      local: t('Install the runtime, clone the repo, configure the agent yourself.'),
      cloud: t('Sign up and the agent is ready. No install, no clone, no setup.'),
    },
    {
      local: t('Wire every model API key by hand, pick providers, babysit rate limits.'),
      cloud: t('Forty-plus models already wired in. Bring your own key only if you want.'),
    },
    {
      local: t('Run it on your own machine: leave it on, pay the power, keep it alive.'),
      cloud: t('We host it. Close the tab, it sleeps free for two hours; reopen, resume.'),
    },
    {
      local: t('It is a command-line agent. You get a terminal, not an interface.'),
      cloud: t('You get the agent, plus a full graphical UI to drive it.'),
    },
  ]

  return (
    <section className='px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-5xl'>
        <AnimateInView animation='fade-up' className='mb-14 max-w-2xl md:mb-16'>
          <h2
            className='text-foreground text-[clamp(1.75rem,3.5vw,2.75rem)] font-medium leading-[1.1] tracking-[-0.03em] [text-wrap:balance]'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('Hermes is open-source, so why ours?')}
          </h2>
          <p className='text-muted-foreground/70 mt-5 text-base leading-relaxed [text-wrap:pretty]'>
            {t(
              'You can run Hermes yourself for free. Here is what that actually takes, and what our cloud removes.'
            )}
          </p>
        </AnimateInView>

        <AnimateInView animation='fade-up' delay={80}>
          <div className='border-border/40 grid grid-cols-1 overflow-hidden border md:grid-cols-2'>
            <div className='border-border/40 bg-muted/10 p-7 md:p-9'>
              <p className='text-muted-foreground/60 mb-5 font-mono text-[11px] tracking-[0.2em] uppercase'>
                {t('Self-host (free)')}
              </p>
              <ul className='space-y-4'>
                {rows.map((r, i) => (
                  <li key={i} className='text-muted-foreground text-sm leading-relaxed [text-wrap:pretty]'>
                    {r.local}
                  </li>
                ))}
              </ul>
            </div>
            <div className='border-border/40 border-t bg-background p-7 md:border-l md:border-t-0 md:p-9'>
              <p className='text-warning mb-5 font-mono text-[11px] tracking-[0.2em] uppercase'>
                {t('Savvy cloud (2h free)')}
              </p>
              <ul className='space-y-4'>
                {rows.map((r, i) => (
                  <li key={i} className='text-foreground/90 text-sm leading-relaxed [text-wrap:pretty]'>
                    {r.cloud}
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
