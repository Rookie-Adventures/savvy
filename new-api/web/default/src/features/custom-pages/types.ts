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

export type CustomPageSlug = 'Product' | 'Faq' | 'Refund' | 'Contact' | 'OpenSource'

export const CUSTOM_PAGE_SLUGS: CustomPageSlug[] = [
  'Product',
  'Faq',
  'Refund',
  'Contact',
  'OpenSource',
]

export const CUSTOM_PAGE_ROUTES: Record<CustomPageSlug, string> = {
  Product: '/product',
  Faq: '/faq',
  Refund: '/refund',
  Contact: '/contact',
  OpenSource: '/open-source',
}

export const CUSTOM_PAGE_LABELS: Record<CustomPageSlug, string> = {
  Product: 'Product',
  Faq: 'FAQ',
  Refund: 'Refund Policy',
  Contact: 'Contact',
  OpenSource: 'Open Source',
}
