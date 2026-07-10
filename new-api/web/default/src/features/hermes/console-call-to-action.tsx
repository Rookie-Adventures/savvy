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
import { useTranslation } from 'react-i18next'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'

/**
 * ConsoleCallToAction — commercial block rendered below the workspace
 * control card on the /hermes console page.
 *
 * Spec: docs/superpowers/specs/2026-07-10-console-call-to-action-design.md
 */

const PLANS = [
  {
    nameKey: 'FREE',
    storage: '5 GB',
    pitchKey: 'Free out of the box, 2 hours per session, data secure',
    color: 'text-muted-foreground',
    borderColor: 'border-border',
    isCurrent: true,
    ctaKey: 'Current plan',
    href: '',
  },
  {
    nameKey: 'STARTER',
    storage: '2.00 vCPU · 2g',
    pitchKey: 'Stay online in the background, keep tasks running long-term',
    color: 'text-blue-500',
    borderColor: 'border-blue-500/30',
    isCurrent: false,
    ctaKey: 'Upgrade to STARTER',
    href: '/wallet',
  },
  {
    nameKey: 'PRO',
    storage: '2.00 vCPU · 4g · 50G',
    pitchKey: 'Multiple workspaces in parallel, full specs for heavy workloads',
    color: 'text-primary',
    borderColor: 'border-primary/30',
    isCurrent: false,
    ctaKey: 'Upgrade to PRO',
    href: '/wallet',
  },
]

const STEPS = [
  {
    num: '1',
    titleKey: 'Generate API Key',
    descKey: 'Create a key that belongs to you on the token page',
    href: '/keys',
  },
  {
    num: '2',
    titleKey: 'Fill in Workspace Settings',
    descKey: 'Paste the key into workspace settings to bind your dedicated channel',
    href: null, // step 2 is the workspace start dialog — no separate settings page
  },
  {
    num: '3',
    titleKey: 'Awaken Workspace',
    descKey: 'Once the key is in place, your cloud workspace is ready to go',
    href: null,
  },
]

export function ConsoleCallToAction() {
  const { t } = useTranslation()

  return (
    <div className='flex flex-col gap-6'>
      {/* ── 1. Steps ── */}
      <div className='flex flex-col gap-3'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'First time? Three steps to awaken your dedicated workspace, free out of the box.'
          )}
        </p>
        <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
          {STEPS.map((step) => {
            const inner = (
              <div className='flex items-start gap-3 p-4'>
                <div className='bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center rounded-full text-xs font-bold'>
                  {step.num}
                </div>
                <div className='flex flex-col gap-0.5'>
                  <span className='text-foreground text-sm font-semibold'>
                    {t(step.titleKey)}
                  </span>
                  <span className='text-muted-foreground text-xs'>
                    {t(step.descKey)}
                  </span>
                </div>
              </div>
            )

            if (step.href) {
              return (
                <Card
                  key={step.num}
                  className='hover:border-primary/40 transition-colors'
                >
                  <Link to={step.href} className='block'>
                    {inner}
                  </Link>
                </Card>
              )
            }

            return (
              <Card key={step.num}>
                {inner}
              </Card>
            )
          })}
        </div>
      </div>

      {/* ── 2. Upgrade intro ── */}
      <p className='text-muted-foreground text-sm'>
        {t(
          'Use anywhere, no need to switch devices or migrate — your data stays private'
        )}
      </p>

      {/* ── 3. Plan cards ── */}
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
        {PLANS.map((plan) => (
          <Card
            key={plan.nameKey}
            className={`flex flex-col border-t-[3px] ${plan.borderColor}`}
          >
            <CardContent className='flex flex-1 flex-col gap-3 p-5'>
              <span className={`text-xs font-bold uppercase tracking-widest ${plan.color}`}>
                {plan.nameKey}
              </span>
              <span className='text-foreground text-2xl font-bold'>
                {plan.storage}
              </span>
              <p className='text-muted-foreground flex-1 text-sm'>
                {t(plan.pitchKey)}
              </p>
              {plan.isCurrent ? (
                <Badge variant='secondary' className='w-fit'>
                  {t(plan.ctaKey)}
                </Badge>
              ) : (
                <Button
                  size='sm'
                  render={<Link to={plan.href} />}
                >
                  {t(plan.ctaKey)}
                </Button>
              )}
            </CardContent>
          </Card>
        ))}
      </div>

      {/* ── 4. Disclaimer ── */}
      <p className='text-muted-foreground/60 text-xs leading-relaxed'>
        {t(
          'HermesAgent is an open-source AI Agent. Please fully evaluate its security and stability before use and strictly follow the license agreement to ensure system and data safety.'
        )}
      </p>
    </div>
  )
}
