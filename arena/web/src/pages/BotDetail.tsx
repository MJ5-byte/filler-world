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
  const failed = bot.status === 'failed'
  const stageIdx = { pending: 0, building: 1, active: 2, failed: 2 }[bot.status] ?? -1

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
            {(['PENDING', 'BUILDING', failed ? 'FAILED' : 'ACTIVE'] as const).map((label, i) => {
              // pending/building are transient states the bot is "in" (pulse);
              // active/failed are terminal outcomes of the last step (settled).
              const state = i < stageIdx ? 'done'
                : i === stageIdx ? (failed ? 'failed' : bot.status === 'active' ? 'done' : 'current')
                : ''
              return (
                <div className="build-step" key={label}>
                  <div className="build-step-dot-wrap">
                    <div className={`build-step-dot ${state}`} />
                    <div className={`build-step-label ${state}`}>{label}</div>
                  </div>
                  {i < 2 && <div className={`build-step-line ${i < stageIdx ? 'done' : ''}`} />}
                </div>
              )
            })}
          </div>
          {failed && buildLog && (
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

      <h2>Match history</h2>
      <MatchTable matches={matches} perspectiveBotId={bot.id} />
    </>
  )
}
