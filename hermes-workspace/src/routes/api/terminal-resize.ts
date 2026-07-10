import { createFileRoute } from '@tanstack/react-router'
import { requireLocalOrAuth } from '../../server/auth-middleware'
import { getTerminalSession } from '../../server/terminal-sessions'
import { requireJsonContentType } from '../../server/rate-limit'

export const Route = createFileRoute('/api/terminal-resize')({
  server: {
    handlers: {
      POST: async ({ request }) => {
        if (!requireLocalOrAuth(request)) {
          return new Response(
            JSON.stringify({ ok: false, error: 'Unauthorized' }),
            {
              status: 401,
              headers: { 'Content-Type': 'application/json' },
            },
          )
        }
        const csrfCheck = requireJsonContentType(request)
        if (csrfCheck) return csrfCheck

        const body = (await request.json().catch(() => ({}))) as Record<
          string,
          unknown
        >
        const sessionId =
          typeof body.sessionId === 'string' ? body.sessionId.trim() : ''
        const colsRaw = typeof body.cols === 'number' ? body.cols : 80
        const rowsRaw = typeof body.rows === 'number' ? body.rows : 24
        const cols = Math.max(20, Math.min(500, Math.floor(colsRaw)))
        const rows = Math.max(5, Math.min(300, Math.floor(rowsRaw)))
        if (!sessionId) {
          return new Response(
            JSON.stringify({ ok: false, error: 'sessionId required' }),
            {
              status: 400,
              headers: { 'Content-Type': 'application/json' },
            },
          )
        }
        const session = getTerminalSession(sessionId)
        if (!session) {
          // ponytail: resize is an idempotent notification — the terminal may not
          // exist yet (tab just switched) or may already be closed. Returning a
          // real 404 here spams the console on every height/activeTab change.
          // Swallow: the next resize after the session comes up applies the size.
          return new Response(JSON.stringify({ ok: true, session: 'pending' }), {
            headers: { 'Content-Type': 'application/json' },
          })
        }
        session.resize(cols, rows)
        return new Response(JSON.stringify({ ok: true }), {
          headers: { 'Content-Type': 'application/json' },
        })
      },
    },
  },
})
