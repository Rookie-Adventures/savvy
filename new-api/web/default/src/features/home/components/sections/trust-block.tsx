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
import { AnimateInView } from '@/components/animate-in-view'
import { ProjectAttribution } from '@/components/layout/components/footer'

interface TrustBlockProps {
  className?: string
}

// ponytail: single footer-line replacement — legal subject + registered office
// on the left, ICP / public-security filings + the protected project
// attribution on the right. Replaces the global <Footer/> on the home page so
// the two never co-render (which produced a double-footer). Everything is
// text-xs / muted like a copyright row, with inline "主体 ·" / "合规 ·" leads
// rather than stacked block headings — labels live on the line, so it reads as
// one quiet colophon, not a labelled block (the kind of stacking that looked
// disconnected last pass). Filing numbers are invariant Chinese identifiers
// shown in zh regardless of UI language. Contact + payment copy deliberately
// omitted (no-contact, no-payment-pitch; the free trial IS the trust signal).
// ponytail: badges kept at brand color (no tint) — regulatory seal; muting it
// weakens the only verifiable trust signal. Files in public/ → served as-is,
// out of the bundle.
const ENTITY = '郑州市管城回族区栗橙网络科技工作室(个体工商户)'
const OFFICE = '河南省郑州市管城回族区航海东路12号4号楼2单元6层605号'
const ICP = { no: '豫ICP备2026026934号', href: 'https://beian.miit.gov.cn/', badge: '/gongxin.png' }
const PSB = { no: '豫公网安备 41010402003621号', href: 'https://beian.mps.gov.cn/', badge: '/beian.png' }

function LeadLabel(props: { children: string }) {
  return (
    <span className='text-muted-foreground-faint font-mono text-[11px] tracking-[0.1em] uppercase'>
      {props.children}
    </span>
  )
}

function FilingLink(props: { no: string; href: string; badge: string }) {
  return (
    <a
      href={props.href}
      target='_blank'
      rel='noopener noreferrer'
      className='inline-flex items-center gap-1.5 whitespace-nowrap hover:text-foreground underline-offset-4 transition-colors duration-200 hover:underline'
    >
      <img
        src={props.badge}
        alt=''
        aria-hidden='true'
        className='h-3.5 w-auto shrink-0'
      />
      {props.no}
    </a>
  )
}

export function TrustBlock(_props: TrustBlockProps) {
  const currentYear = new Date().getFullYear()

  return (
    <section className='border-border/40 border-t px-6 py-12 md:py-16'>
      <div className='mx-auto max-w-5xl'>
        <AnimateInView animation='fade-up'>
          <div className='grid grid-cols-1 gap-x-12 gap-y-6 md:grid-cols-2 md:gap-16'>
            {/* Left: legal subject + registered office */}
            <address className='text-muted-foreground-soft flex flex-col gap-1.5 text-xs leading-relaxed not-italic sm:text-start'>
              <LeadLabel>主体</LeadLabel>
              <span>{ENTITY}</span>
              <span>{OFFICE}</span>
            </address>

            {/* Right: ICP / public-security filings + protected project attribution */}
            <div className='text-muted-foreground-soft flex flex-col items-start gap-1.5 text-xs'>
              <LeadLabel>合规</LeadLabel>
              <div className='flex flex-wrap items-center gap-x-1.5 gap-y-1'>
                <FilingLink no={ICP.no} href={ICP.href} badge={ICP.badge} />
                <span aria-hidden='true' className='text-muted-foreground-faint'>·</span>
                <FilingLink no={PSB.no} href={PSB.href} badge={PSB.badge} />
              </div>
              <ProjectAttribution currentYear={currentYear} inline />
            </div>
          </div>
        </AnimateInView>
      </div>
    </section>
  )
}
