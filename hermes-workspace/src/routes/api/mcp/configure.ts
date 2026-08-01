import { createFileRoute } from '@tanstack/react-router'
import { json } from '@tanstack/react-start'
import { isAuthenticated } from '../../../server/auth-middleware'
import {
  BEARER_TOKEN,
  CLAUDE_API,
  CLAUDE_UPGRADE_INSTRUCTIONS,
  dashboardFetch,
  ensureGatewayProbed,
  getCapabilities,
} from '../../../server/gateway-capabilities'
import { requireJsonContentType, safeErrorMessage } from '../../../server/rate-limit'
import {
  maskSecretsInPlace,
  normalizeMcpServer,
  normalizeMcpServerFromConfig,
} from '../../../server/mcp-normalize'
import { getConfig, saveConfig } from '../../../server/claude-dashboard-api'
import type { McpConfigureInput } from '../../../types/mcp-input'
import { createCapabilityUnavailablePayload } from '@/lib/feature-gates'

const REQUEST_TIMEOUT_MS = 30_000

async function mcpFetch(path: string, init: RequestInit): Promise<Response> {
  const capabilities = getCapabilities()
  if (capabilities.dashboard.available) {
    return dashboardFetch(path, init)
  }
  const headers = new Headers(init.headers)
  if (BEARER_TOKEN && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${BEARER_TOKEN}`)
  }
  return fetch(`${CLAUDE_API}${path}`, { ...init, headers })
}

function readConfigure(raw: unknown): McpConfigureInput | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  const name = typeof r.name === 'string' ? r.name.trim() : ''
  if (!name) return null
  const out: McpConfigureInput = { name }
  if (typeof r.enabled === 'boolean') out.enabled = r.enabled
  if (r.toolMode === 'all' || r.toolMode === 'include' || r.toolMode === 'exclude') {
    out.toolMode = r.toolMode
  }
  if (Array.isArray(r.includeTools)) {
    out.includeTools = (r.includeTools as Array<unknown>).map((t) => String(t))
  }
  if (Array.isArray(r.excludeTools)) {
    out.excludeTools = (r.excludeTools as Array<unknown>).map((t) => String(t))
  }
  return out
}

export const Route = createFileRoute('/api/mcp/configure')({
  server: {
    handlers: {
      PUT: async ({ request }) => {
        if (!isAuthenticated(request)) {
          return json({ ok: false, error: 'Unauthorized' }, { status: 401 })
        }
        const csrfCheck = requireJsonContentType(request)
        if (csrfCheck) return csrfCheck
        const capabilities = await ensureGatewayProbed()
        if (!capabilities.mcp && !capabilities.mcpFallback) {
          return json(
            createCapabilityUnavailablePayload('mcp', {
              error: `Gateway does not support /api/mcp. ${CLAUDE_UPGRADE_INSTRUCTIONS}`,
            }),
            { status: 503 },
          )
        }
        try {
          const raw = (await request.json()) as unknown
          const input = readConfigure(raw)
          if (!input) {
            return json({ ok: false, error: 'Invalid configure payload' }, { status: 400 })
          }
          if (capabilities.mcp) {
            const nameEnc = encodeURIComponent(input.name)
            const hasToolConfig =
              input.toolMode !== undefined ||
              Array.isArray(input.includeTools) ||
              Array.isArray(input.excludeTools)

            // Upstream exposes no single "configure" endpoint. enabled toggles
            // via /servers/{name}/enabled; tool selection (toolMode/include/
            // exclude) is persisted by whole-map replace (PUT /servers), which
            // writes tool_mode/include_tools/exclude_tools onto the entry.
            let latest: Response | null = null

            if (typeof input.enabled === 'boolean') {
              const r = await mcpFetch(`/api/mcp/servers/${nameEnc}/enabled`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enabled: input.enabled }),
                signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
              })
              if (!r.ok) {
                const body = (await r.json().catch(() => ({}))) as Record<string, unknown>
                const errMsg =
                  (body.error as string | undefined) ||
                  `MCP configure failed (${r.status})`
                return json({ ok: false, error: errMsg }, { status: r.status || 502 })
              }
              latest = r
            }

            if (hasToolConfig) {
              // Whole-map replace needs the raw config map (headers/bearer/
              // command/args all intact), not the redacted /servers summary.
              // GET /api/config returns mcp_servers verbatim, so we patch one
              // entry's tool selection and PUT the whole map back. Upstream
              // /servers is a clean replace (deepMerge can't drop keys), so
              // missing fields would wipe a server's transport/auth — every
              // entry must be carried through unchanged.
              const cfgRes = await mcpFetch('/api/config', {
                signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
              })
              if (!cfgRes.ok) {
                const body = (await cfgRes.json().catch(() => ({}))) as Record<string, unknown>
                const errMsg =
                  (body.error as string | undefined) ||
                  `MCP configure failed (${cfgRes.status})`
                return json({ ok: false, error: errMsg }, { status: cfgRes.status || 502 })
              }
              const cfgRaw = (await cfgRes.json().catch(() => ({}))) as Record<string, unknown>
              const rootCfg: Record<string, unknown> =
                cfgRaw.config && typeof cfgRaw.config === 'object'
                  ? (cfgRaw.config as Record<string, unknown>)
                  : cfgRaw
              const rawMap = rootCfg.mcp_servers
              const servers =
                rawMap && typeof rawMap === 'object' && !Array.isArray(rawMap)
                  ? { ...(rawMap as Record<string, Record<string, unknown>>) }
                  : {}
              if (!(input.name in servers)) {
                return json(
                  { ok: false, error: `MCP server not found: ${input.name}` },
                  { status: 404 },
                )
              }
              const next: Record<string, unknown> = { ...servers[input.name] }
              delete next.tool_mode
              delete next.include_tools
              delete next.exclude_tools
              // Upstream schema: one `tools` key holding the enabled tool-name
              // allow-list, or absent/None for "all tools enabled". There is no
              // exclude/blacklist mode — the agent only supports opting tools IN.
              // We reject `exclude` to avoid silently dropping the intent.
              if (input.toolMode === 'exclude') {
                return json(
                  {
                    ok: false,
                    error:
                      'Tool exclude mode is not supported by the agent (only allow-list / all).',
                  },
                  { status: 400 },
                )
              }
              if (input.toolMode === 'include' && Array.isArray(input.includeTools)) {
                if (input.includeTools.length > 0) {
                  next.tools = input.includeTools
                } else {
                  delete next.tools
                }
              } else {
                // 'all' (or no toolMode): enable every tool.
                delete next.tools
              }
              servers[input.name] = next
              const r = await mcpFetch('/api/mcp/servers', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ servers }),
                signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
              })
              if (!r.ok) {
                const body = (await r.json().catch(() => ({}))) as Record<string, unknown>
                const errMsg =
                  (body.error as string | undefined) ||
                  `MCP configure failed (${r.status})`
                return json({ ok: false, error: errMsg }, { status: r.status || 502 })
              }
              latest = r
            }

            if (!latest) {
              return json({ ok: false, error: 'Nothing to configure' }, { status: 400 })
            }
            // Return a best-effort patched summary; upstream /enabled and
            // /servers PUT return {ok:true} without the server shape, so
            // re-read the one entry to give the UI a fresh normalized view.
            const after = await mcpFetch('/api/mcp/servers', {
              signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
            })
            if (after.ok) {
              const all = (await after.json().catch(() => ({ servers: [] }))) as {
                servers?: Array<unknown>
              }
              const found =
                (all.servers ?? []).find(
                  (s) => (s as Record<string, unknown>).name === input.name,
                ) ?? null
              const server = found ? normalizeMcpServer(found) : null
              if (server) {
                return json({ ok: true, server: maskSecretsInPlace(server) })
              }
            }
            return json({ ok: true })
          }
          // Phase 1.5 fallback — patch the matching `config.mcp_servers[name]`
          // entry in place. We only update the toggleable keys exposed by
          // McpConfigureInput; transport/secrets stay untouched.
          const cfg = await getConfig()
          const root: Record<string, unknown> =
            'config' in cfg && cfg.config && typeof cfg.config === 'object'
              ? (cfg.config as Record<string, unknown>)
              : cfg
          const rawServers = root.mcp_servers
          const servers =
            rawServers && typeof rawServers === 'object' && !Array.isArray(rawServers)
              ? { ...(rawServers as Record<string, unknown>) }
              : {}
          const existing = servers[input.name]
          if (!existing || typeof existing !== 'object' || Array.isArray(existing)) {
            return json({ ok: false, error: `MCP server not found: ${input.name}` }, { status: 404 })
          }
          const next: Record<string, unknown> = { ...(existing as Record<string, unknown>) }
          if (typeof input.enabled === 'boolean') next.enabled = input.enabled
          if (input.toolMode) next.tool_mode = input.toolMode
          if (Array.isArray(input.includeTools)) next.include_tools = input.includeTools
          if (Array.isArray(input.excludeTools)) next.exclude_tools = input.excludeTools
          servers[input.name] = next
          await saveConfig({ mcp_servers: servers })
          const written = normalizeMcpServerFromConfig(input.name, next)
          if (!written) {
            return json({ ok: false, error: 'MCP configure failed (config write)' }, { status: 500 })
          }
          return json({ ok: true, server: maskSecretsInPlace(written) })
        } catch (err) {
          return json({ ok: false, error: safeErrorMessage(err) }, { status: 500 })
        }
      },
    },
  },
})
