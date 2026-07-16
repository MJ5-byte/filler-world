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

      {bot.status === 'failed' && buildLog && (
        <>
          <h2>Build failed</h2>
          <div className="log-box">{buildLog}</div>
        </>
      )}
      {(bot.status === 'pending' || bot.status === 'building') && (
        <p className="muted">Building in the sandbox… this page refreshes automatically.</p>
      )}

      <h2>Match history</h2>
      <MatchTable matches={matches} perspectiveBotId={bot.id} />
    </>
  )
}
