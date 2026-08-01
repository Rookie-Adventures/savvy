import { createFileRoute } from '@tanstack/react-router'
import { json } from '@tanstack/react-start'
import { isAuthenticated } from '../../../server/auth-middleware'
import {
  CLAUDE_UPGRADE_INSTRUCTIONS,
  ensureGatewayProbed,
} from '../../../server/gateway-capabilities'
import { requireJsonContentType, safeErrorMessage } from '../../../server/rate-limit'
import { parseMcpServerInput } from '../../../server/mcp-input-validate'
import { createCapabilityUnavailablePayload } from '@/lib/feature-gates'

export const Route = createFileRoute('/api/mcp/discover')({
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
          // Phase 1.5: live discover requires the runtime endpoint.
          return json({
            ok: false,
            status: 'unknown',
            discoveredTools: [],
            error:
              'Live test/discover requires hermes-agent /api/mcp runtime endpoint, not yet available on this dashboard.',
          })
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
          const raw = (await request.json()) as unknown
          const parsed = parseMcpServerInput(raw)
          if (!parsed.ok) {
            return json(
              { ok: false, error: 'Invalid MCP discover payload', errors: parsed.errors },
              { status: 400 },
            )
          }
          // Upstream agent has no ad-hoc discover endpoint; the live test
          // path (`POST /api/mcp/servers/{name}/test`) only probes servers
          // already saved by name. To discover a server's tools before
          // saving, create it then test by name.
          return json(
            {
              ok: false,
              status: 'unknown',
              discoveredTools: [],
              error:
                'Live discover requires a saved server. Create the server first, then test it by name to list its tools.',
            },
            { status: 503 },
          )
        } catch (err) {
          return json({ ok: false, tools: [], error: safeErrorMessage(err) }, { status: 500 })
        }
      },
    },
  },
})
