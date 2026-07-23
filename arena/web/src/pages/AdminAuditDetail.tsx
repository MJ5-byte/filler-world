import { useCallback, useEffect, useState } from 'react'
import { Link, useOutletContext, useParams } from 'react-router-dom'
import { api, AuditDetail } from '../api'
import type { AppContext } from '../App'
import LangBadge from '../components/LangBadge'

const GATES: { key: 'map00' | 'map01' | 'map02' | 'bonus'; opponent: string; map: string; required: boolean }[] = [
  { key: 'map00', opponent: 'wall_e', map: 'map00', required: true },
  { key: 'map01', opponent: 'h2_d2', map: 'map01', required: true },
  { key: 'map02', opponent: 'bender', map: 'map02', required: true },
  { key: 'bonus', opponent: 'terminator', map: '—', required: false },
]

// Same "wins >= 4" threshold used for the compact win/loss coloring on the
// admin audits list — there's no explicit per-gate pass field on the wire,
// so this is the one source of truth for both views.
function gatePass(wins: number, losses: number): boolean | null {
  if (wins + losses === 0) return null
  return wins >= 4
}

export default function AdminAuditDetail() {
  const { botId } = useParams()
  const { user, authReady } = useOutletContext<AppContext>()
  const [detail, setDetail] = useState<AuditDetail | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [notes, setNotes] = useState('')

  const load = useCallback(() => {
    if (!botId) return
    return api.adminAuditDetail(Number(botId))
      .then(d => {
        setDetail(d)
        setNotes(d.notes ?? '')
      })
      .catch(e => setError(String(e instanceof Error ? e.message : e)))
  }, [botId])

  useEffect(() => {
    if (!user?.isAdmin) return
    load()
  }, [user, load])

  // Only auto-poll while the automated stage is still running — once the bot
  // reaches needs_review the admin may be mid-edit on notes, and a
  // background refetch would stomp on that.
  useEffect(() => {
    if (!detail || detail.auditStatus !== 'running') return
    const t = setInterval(load, 4000)
    return () => clearInterval(t)
  }, [detail, load])

  if (!authReady) return <p className="muted">Loading…</p>
  if (!user?.isAdmin) {
    return (
      <div className="panel center-note">
        <h1>Admin</h1>
        <p className="muted">This area is for arena admins only.</p>
      </div>
    )
  }
  if (error) return <div className="error-box">{error}</div>
  if (!detail) return <p className="muted">Loading…</p>

  const editable = detail.auditStatus === 'needs_review'

  const decide = (decision: 'accept' | 'reject') => {
    const msg = decision === 'accept'
      ? `Accept ${detail.botName}? This makes the bot live and puts it on the leaderboard.`
      : `Reject ${detail.botName}? This cannot be undone from here.`
    if (!confirm(msg)) return
    setNotice('')
    api.adminDecideAudit(detail.botId, decision, notes)
      .then(() => { setNotice(`Bot ${decision === 'accept' ? 'accepted' : 'rejected'}.`); load() })
      .catch(e => setNotice(String(e instanceof Error ? e.message : e)))
  }

  return (
    <>
      <div className="page-head">
        <h1>
          {detail.botName}{' '}
          <span className={`badge ${detail.auditStatus}`}>{detail.auditStatus.replace('_', ' ')}</span>
        </h1>
        <Link to="/admin"><button className="ghost small">Back to admin</button></Link>
      </div>

      {notice && <div className="panel" style={{ marginBottom: 14 }}>{notice}</div>}

      {detail.auditStatus === 'awaiting_submit' && (
        <div className="panel" style={{ marginBottom: 14 }}>
          Passed the automated gates, but the owner hasn't submitted it for review yet — it'll show up
          in the main Bot Audits queue once they do.
        </div>
      )}

      <div className="panel">
        <div className="score-row">
          <span className="muted">owner</span> <Link to={`/players/${detail.owner}`}>{detail.owner}</Link>
          <span className="muted">language</span> <LangBadge lang={detail.language} />
          <span className="muted">bot page</span> <Link to={`/bots/${detail.botId}`}>view public page</Link>
          <span className="muted">automated checks</span>{' '}
          {detail.automatedPassed == null
            ? <span className="badge running">running</span>
            : detail.automatedPassed
              ? <span className="badge active">passed</span>
              : <span className="badge rejected">failed</span>}
        </div>
      </div>

      <h2>Automated results</h2>
      <table>
        <thead>
          <tr>
            <th>Gate</th><th>Opponent</th><th className="num">Wins</th><th className="num">Losses</th>
            <th>Required</th><th>Pass</th>
          </tr>
        </thead>
        <tbody>
          {GATES.map(g => {
            const gate = detail.gates[g.key]
            const pass = gatePass(gate.wins, gate.losses)
            return (
              <tr key={g.key}>
                <td>{g.key}</td>
                <td className="muted">{gate.opponent || g.opponent}</td>
                <td className="num result-win">{gate.wins}</td>
                <td className="num result-loss">{gate.losses}</td>
                <td className="muted">{g.required ? 'yes' : 'informational'}</td>
                <td>
                  {!g.required
                    ? <span className="muted">—</span>
                    : pass == null
                      ? <span className="muted">—</span>
                      : pass ? <span className="result-win">pass</span> : <span className="result-loss">fail</span>}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>

      {detail.automatedError && (
        <div className="error-box" style={{ marginTop: 14 }}>{detail.automatedError}</div>
      )}

      <h2>Source</h2>
      {detail.source != null
        ? <div className="log-box" style={{ maxHeight: 420 }}>{detail.source}</div>
        : <p className="muted">Source not available (binary upload).</p>}

      <h2>Build log</h2>
      {detail.buildLog != null
        ? <div className="log-box">{detail.buildLog}</div>
        : <p className="muted">No build log.</p>}

      <h2>Notes</h2>
      <textarea
        rows={6}
        style={{ width: '100%' }}
        value={notes}
        onChange={e => setNotes(e.target.value)}
        disabled={!editable}
        placeholder="Review notes…"
      />
      {detail.decidedAt && (
        <p className="muted small-num" style={{ marginTop: 8 }}>
          Decided by {detail.reviewer ?? '—'} on {new Date(detail.decidedAt).toLocaleString()}.
        </p>
      )}

      {editable && (
        <div className="admin-actions" style={{ marginTop: 14, justifyContent: 'flex-start' }}>
          <button onClick={() => decide('accept')}>Accept bot</button>
          <button className="danger" onClick={() => decide('reject')}>Reject bot</button>
        </div>
      )}
    </>
  )
}
