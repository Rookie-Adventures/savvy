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

interface FeaturesProps {
  className?: string
}

// ponytail: old Features stacked two grids — a numbered (01–04) bento plus a
// 4-up icon card row — the "identical card grid" + "numbered markers" tells,
// both black-listed. Replaced with an editorial "what's inside" ruled list:
// serif subject lines + plain support text, hairline separators, no icons.
// API capacity is mentioned once, quietly, as something already inside the
// workspace — matching the "API as a wrapped sub-sell" product posture.
export function Features(_props: FeaturesProps) {
  const { t } = useTranslation()

  // ponytail: occupation-led copy — each line names a real job (listing,
  // draft, design) instead of abstract SaaS verbs. The product is a work
  // agent for people who ship, not an "AI gateway". API stays a quiet sub:
  // one line, "the models are already there", no gateway language.
  const items = [
    {
      title: t('Built for whatever you ship'),
      body: t(
        'A listing that needs copy, a script that needs notes, a design that needs a pass. Open the right agent, do the work, close it.'
      ),
    },
    {
      title: t('Each agent keeps to itself'),
      body: t(
        'One agent, one job, one context: your runtime, your history, your files, unseen by anyone else on the stack.'
      ),
    },
    {
      title: t('The models are already there'),
      body: t(
        'Forty-plus models behind one entry, ready in the agent. Use what is wired in, or bring your own key.'
      ),
    },
    {
      title: t('Your work survives the close'),
      body: t(
        'Shut the tab and the agent sleeps. Open it again from any browser and pick up where you stopped.'
      ),
    },
  ]

  return (
    <section className='border-border/40 px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-5xl'>
        <AnimateInView animation='fade-up' className='mb-16 max-w-xl md:mb-20'>
          <h2
            className='text-foreground text-[clamp(1.75rem,3.5vw,2.75rem)] font-medium leading-[1.1] tracking-[-0.03em] [text-wrap:balance]'
            style={{ fontFamily: 'var(--font-serif)' }}
          >
            {t('What each agent carries')}
          </h2>
        </AnimateInView>

        <div className='border-border/40 divide-border/40 divide-y'>
          {items.map((item, i) => (
            <AnimateInView
              key={item.title}
              animation='fade-up'
              delay={i * 80}
              className='grid grid-cols-1 gap-3 py-8 md:grid-cols-12 md:gap-8 md:py-10'
            >
              <h3
                className='text-foreground text-lg leading-snug font-medium md:col-span-5 [text-wrap:balance]'
                style={{ fontFamily: 'var(--font-serif)' }}
              >
                {item.title}
              </h3>
              <p className='text-muted-foreground/80 text-base leading-relaxed md:col-span-7 [text-wrap:pretty]'>
                {item.body}
              </p>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
