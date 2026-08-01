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
import { normalizeTestResult } from '../../../server/mcp-normalize'
import { runHermesMcpTest } from '../../../server/mcp-cli-bridge'
import { setProbe } from '../../../server/mcp-tools-cache'
import { createCapabilityUnavailablePayload } from '@/lib/feature-gates'

const TEST_TIMEOUT_MS = 30_000

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

export const Route = createFileRoute('/api/mcp/test')({
  server: {
    handlers: {
      POST: async ({ request }) => {
        if (!isAuthenticated(request)) {
          return json({ ok: false, error: 'Unauthorized' }, { status: 401 })
        }
        const csrfCheck = requireJsonContentType(request)
        if (csrfCheck) return csrfCheck
        const capabilities = await ensureGatewayProbed()
        if (capabilities.mcpFallback && !capabilities.mcp) {
          // Phase 1.5 fallback: shell out to `hermes mcp test <name>` and
          // parse stdout. Reuses the CLI's _probe_single_server logic
          // without duplicating MCP protocol handling on the workspace
          // side. Only the by-name form is supported (config-only mode);
          // ad-hoc client-input tests still need the runtime endpoint.
          try {
            const raw = (await request.json()) as Record<string, unknown>
            const name = typeof raw.name === 'string' ? raw.name : null
            if (!name) {
              return json({
                ok: false,
                status: 'unknown',
                discoveredTools: [],
                error:
                  'Local fallback only supports testing existing servers by name.',
              })
            }
            const result = await runHermesMcpTest(name, { timeoutMs: TEST_TIMEOUT_MS })
            setProbe(name, {
              status: result.status,
              toolCount: result.discoveredTools.length,
              toolNames: result.discoveredTools.map((t) => t.name),
              latencyMs: result.latencyMs,
              error: result.error,
            })
            return json(result)
          } catch (err) {
            return json(
              {
                ok: false,
                status: 'failed',
                discoveredTools: [],
                error: safeErrorMessage(err),
              },
              { status: 500 },
            )
          }
        }
        if (!capabilities.mcp) {
          return json(
            createCapabilityUnavailablePayload('mcp', {
              error: `Gateway does not support /api/mcp. ${CLAUDE_UPGRADE_INSTRUCTIONS}`,
            }),
            { status: 503 },
          )
        }
        try {
          const raw = (await request.json()) as Record<string, unknown>
          const name = typeof raw.name === 'string' ? raw.name.trim() : ''
          // Upstream `POST /api/mcp/servers/{name}/test` probes an EXISTING
          // server by name — no ad-hoc transport. If the caller sent anything
          // beyond {name}, it's the ad-hoc form the runtime endpoint can't
          // serve; refuse it rather than 404 on a server that may exist.
          if (!name || Object.keys(raw).length > 1) {
            return json(
              {
                ok: false,
                status: 'unknown',
                discoveredTools: [],
                error:
                  'Live MCP test requires a server already saved by name. Create it first, then test by {name}.',
              },
              { status: 400 },
            )
          }
          const response = await mcpFetch(
            `/api/mcp/servers/${encodeURIComponent(name)}/test`,
            {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({}),
              signal: AbortSignal.timeout(TEST_TIMEOUT_MS),
            },
          )
          const payload = (await response.json().catch(() => ({}))) as unknown
          const result = normalizeTestResult(payload)
          // Persist probe result for the fallback list path to show a fresh
          // tool count without re-probing on every refresh.
          setProbe(name, {
            status: result.status,
            toolCount: result.discoveredTools.length,
            toolNames: result.discoveredTools.map((t) => t.name),
            error: result.error,
          })
          return json(result, { status: response.ok ? 200 : response.status || 502 })
        } catch (err) {
          return json({ ok: false, status: 'failed', discoveredTools: [], error: safeErrorMessage(err) }, { status: 500 })
        }
      },
    },
  },
})
