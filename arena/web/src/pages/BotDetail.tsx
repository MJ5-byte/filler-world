import { useEffect, useState } from 'react'
import { Link, useOutletContext, useParams } from 'react-router-dom'
import { api, Bot, Match } from '../api'
import type { AppContext } from '../App'
import LangBadge from '../components/LangBadge'
import MatchTable from '../components/MatchTable'

export default function BotDetail() {
  const { id } = useParams()
  const { user } = useOutletContext<AppContext>()
  const [bot, setBot] = useState<Bot | null>(null)
  const [buildLog, setBuildLog] = useState<string | null>(null)
  const [matches, setMatches] = useState<Match[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    if (!id) return
    let alive = true
    const load = () =>
      api.bot(id)
        .then(d => { if (alive) { setBot(d.bot); setBuildLog(d.buildLog); setMatches(d.matches) } })
        .catch(e => alive && setError(String(e)))
    load()
    const t = setInterval(load, 4000)
    return () => { alive = false; clearInterval(t) }
  }, [id])

  if (error) return <div className="error-box">{error}</div>
  if (!bot) return <p className="muted">Loading…</p>

  const isMine = user != null && user.login === bot.owner
  // 'failed' (build error) and 'rejected' (didn't clear audit) are both
  // terminal, unhappy outcomes — styled the same in the stepper. The
  // build-fail-log panel below stays specific to 'failed' since rejected
  // bots don't have a buildLog-shaped reason on this public endpoint.
  const terminal = bot.status === 'failed' || bot.status === 'rejected'
  // 'failed' bots never reach auditing — they stall at the BUILDING step
  // (shown in its failed/red state), which is why it maps to the same
  // index as 'building' rather than getting its own stage.
  const stageIdx = { pending: 0, building: 1, failed: 1, auditing: 2, active: 3, rejected: 3 }[bot.status] ?? -1

  return (
    <>
      <div className="page-head">
        <h1>{bot.name} <span className={`badge ${bot.status}`}>{bot.status}</span></h1>
        {isMine && bot.status === 'active' && (
          <Link to={`/challenge?bot=${bot.id}`}><button>⚔️ Challenge someone</button></Link>
        )}
      </div>
      <div className="panel">
        <div className="score-row">
          <span className="muted">owner</span>{' '}
          {bot.owner ? <Link to={`/players/${bot.owner}`}>{bot.owner}</Link> : '—'}
          <span className="muted">language</span> <LangBadge lang={bot.language} />
          <span className="muted">rating</span> <span className="rating">{bot.rating?.toFixed(0) ?? '—'}</span>
          <span className="muted">record</span>
          <span><span className="result-win">{bot.wins ?? 0}W</span> / <span className="result-loss">{bot.losses ?? 0}L</span> / {bot.draws ?? 0}D</span>
        </div>
      </div>

      {stageIdx >= 0 && bot.language !== 'builtin' && (
        <div className="panel" style={{ marginTop: 14 }}>
          <div className="panel-title">Build status</div>
          <div className="build-stepper">
            {(['PENDING', 'BUILDING', 'AUDITING', bot.status === 'rejected' ? 'REJECTED' : 'ACTIVE'] as const).map((label, i) => {
              // pending/building/auditing are transient states the bot is "in" (pulse);
              // active/failed/rejected are terminal outcomes of the matching step (settled).
              // Note: a build failure never reaches AUDITING, so its "REJECTED"-style
              // label only applies to the step it actually failed at (BUILDING) — the
              // final label text stays the aspirational 'ACTIVE' unless the bot was
              // actually rejected at manual review.
              const state = i < stageIdx ? 'done'
                : i === stageIdx ? (terminal ? 'failed' : bot.status === 'active' ? 'done' : 'current')
                : ''
              return (
                <div className="build-step" key={label}>
                  <div className="build-step-dot-wrap">
                    <div className={`build-step-dot ${state}`} />
                    <div className={`build-step-label ${state}`}>{label}</div>
                  </div>
                  {i < 3 && <div className={`build-step-line ${i < stageIdx ? 'done' : ''}`} />}
                </div>
              )
            })}
          </div>
          {bot.status === 'failed' && buildLog && (
            <div className="build-fail-log">
              <div className="build-fail-head">
                <span className="build-fail-badge">FAILED</span>
                <span className="muted small-num">build error</span>
              </div>
              <div className="log-box" style={{ marginTop: 10 }}>{buildLog}</div>
            </div>
          )}
        </div>
      )}
      {(bot.status === 'pending' || bot.status === 'building') && (
        <p className="muted">Building in the sandbox… this page refreshes automatically.</p>
      )}
      {bot.status === 'auditing' && (
        <p className="muted">Passed the build — now playing automated matches against reference bots for review… this page refreshes automatically.</p>
      )}

      <h2>Match history</h2>
      <MatchTable matches={matches} perspectiveBotId={bot.id} />
    </>
  )
}
