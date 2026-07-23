import { useCallback, useEffect, useState } from 'react'
import { Link, useOutletContext, useParams } from 'react-router-dom'
import { api, AuditDetail } from '../api'
import type { AppContext } from '../App'
import LangBadge from '../components/LangBadge'

const CHECKLIST_GROUPS: { title: string; items: { key: string; label: string }[] }[] = [
  {
    title: 'Unit Tests',
    items: [
      { key: 'unitTestsPass', label: 'Do all tests pass without errors?' },
      {
        key: 'unitTestsInputParsing',
        label: 'Are there specific tests for Input Parsing (e.g., verifying the robot correctly reads the Anfield dimensions and the piece shape from stdin)?',
      },
      {
        key: 'unitTestsPlacementValidation',
        label: "Are there tests for Placement Validation (e.g., checking that a move is rejected if it overlaps two of your own cells or one of the opponent's)?",
      },
      {
        key: 'unitTestsBoundaryDetection',
        label: 'Are there tests for Boundary Detection to ensure pieces are never placed partially outside the grid?',
      },
    ],
  },
  {
    title: 'Basic',
    items: [
      { key: 'goodPractices', label: 'Does the code obey the good practices?' },
      { key: 'hasTestFile', label: 'Is there a test file for this code?' },
      { key: 'testsCoverCases', label: 'Are the tests checking each possible case?' },
    ],
  },
  {
    title: 'Bonus',
    items: [
      { key: 'hasVisualizer', label: 'Did the student create a visualizer for the project?' },
    ],
  },
]

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
  const [checklist, setChecklist] = useState<Record<string, boolean>>({})
  const [notes, setNotes] = useState('')

  const load = useCallback(() => {
    if (!botId) return
    return api.adminAuditDetail(Number(botId))
      .then(d => {
        setDetail(d)
        setChecklist(d.checklist ?? {})
        setNotes(d.notes ?? '')
      })
      .catch(e => setError(String(e instanceof Error ? e.message : e)))
  }, [botId])

  useEffect(() => {
    if (!user?.isAdmin) return
    load()
  }, [user, load])

  // Only auto-poll while the automated stage is still running — once the
  // bot reaches needs_review the admin may be mid-edit on the checklist or
  // notes, and a background refetch would stomp on that.
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

  const toggle = (key: string) => {
    if (!editable) return
    const updated = { ...checklist, [key]: !checklist[key] }
    setChecklist(updated)
    api.adminSaveChecklist(detail.botId, updated)
      .then(() => setNotice('Checklist saved.'))
      .catch(e => setNotice(String(e instanceof Error ? e.message : e)))
  }

  const decide = (decision: 'accept' | 'reject') => {
    const msg = decision === 'accept'
      ? `Accept ${detail.botName}? This makes the bot live and schedules ladder matches.`
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

      <h2>Manual checklist</h2>
      {detail.auditStatus === 'running' && (
        <p className="muted">Automated checks still running — the checklist unlocks once the bot reaches manual review.</p>
      )}
      {detail.auditStatus !== 'running' && (
        <div className="panel">
          {!editable && <p className="muted" style={{ marginTop: 0 }}>Read-only — this audit was already decided.</p>}
          {CHECKLIST_GROUPS.map(group => (
            <div key={group.title} className="checklist-group">
              <div className="panel-title">{group.title}</div>
              {group.items.map(item => (
                <label key={item.key} className={`checklist-item${editable ? '' : ' disabled'}`}>
                  <input
                    type="checkbox"
                    checked={checklist[item.key] ?? false}
                    disabled={!editable}
                    onChange={() => toggle(item.key)}
                  />
                  {item.label}
                </label>
              ))}
              {group.title === 'Bonus' && (
                <div className="bonus-gate-note">
                  vs terminator win rate:{' '}
                  <span className="result-win">{detail.gates.bonus.wins}W</span>
                  {' / '}
                  <span className="result-loss">{detail.gates.bonus.losses}L</span>
                  {' (automated, informational only)'}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <h2>Notes</h2>
      <textarea
        rows={6}
        style={{ width: '100%' }}
        value={notes}
        onChange={e => setNotes(e.target.value)}
        disabled={!editable}
        placeholder="Manual review notes…"
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
