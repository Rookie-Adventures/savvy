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
import { Cloud, Server, Database } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AnimateInView } from '@/components/animate-in-view'

interface ProductIntroProps {
  className?: string
}

export function ProductIntro(_props: ProductIntroProps) {
  const { t } = useTranslation()

  const features = [
    {
      icon: <Cloud className='size-6' />,
      title: t('Cloud-Based'),
      description: t(
        'No local deployment required. Access your workspace from anywhere with an internet connection.'
      ),
    },
    {
      icon: <Server className='size-6' />,
      title: t('Independent Runtime'),
      description: t(
        'Each workspace runs in an isolated environment, ensuring stability and security for your AI operations.'
      ),
    },
    {
      icon: <Database className='size-6' />,
      title: t('Data Retention'),
      description: t(
        'Your data is securely stored and preserved. Access your history, configurations, and assets anytime.'
      ),
    },
  ]

  return (
    <section className='relative z-10 px-6 py-20 md:py-28'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='text-center' animation='fade-up'>
          <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-3xl'>
            {t('Built for the Cloud')}
          </h2>
          <p className='text-muted-foreground mx-auto mt-4 max-w-xl text-sm leading-relaxed md:text-base'>
            {t(
              'A complete AI workspace that runs entirely in the cloud. Start using it immediately without any setup.'
            )}
          </p>
        </AnimateInView>

        <div className='mt-14 grid grid-cols-1 gap-8 md:grid-cols-3 md:gap-12'>
          {features.map((feature, index) => (
            <AnimateInView
              key={feature.title}
              className='flex flex-col items-center text-center'
              animation='fade-up'
              delay={index * 100}
            >
              <div className='border-border/50 bg-muted/30 mb-5 flex size-12 items-center justify-center rounded-xl border'>
                {feature.icon}
              </div>
              <h3 className='mb-2 text-base font-semibold'>{feature.title}</h3>
              <p className='text-muted-foreground text-sm leading-relaxed'>
                {feature.description}
              </p>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
