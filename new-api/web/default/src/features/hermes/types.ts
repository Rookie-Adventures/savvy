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
export interface ApiResponse<T = unknown> {
  success: boolean
  message: string
  data?: T
}

export interface HermesInstance {
  id: string
  status: 'running' | 'sleeping' | 'creating' | 'error'
  plan: string
  lastError?: string
  accessUrl?: string
  createdAt?: string
  updatedAt?: string
}

export interface HermesAccessToken {
  token: string
  workspaceUrl: string
  expiresAt: string
}

export interface HermesProviderState {
  source: 'ours' | 'user' | 'none'
  model: string | null
  keySetAt: string | null
}

export interface StartHermesInstancePayload {
  providerApiKey: string
  providerBaseUrl?: string
  providerModel?: string
}
