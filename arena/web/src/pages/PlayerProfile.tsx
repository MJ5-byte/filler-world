import { useEffect, useState } from 'react'
import { Link, useOutletContext, useParams } from 'react-router-dom'
import { api, Bot, Match, Player } from '../api'
import type { AppContext } from '../App'
import LangBadge from '../components/LangBadge'
import MatchTable from '../components/MatchTable'
import PlayerAnalytics from '../components/PlayerAnalytics'

export default function PlayerProfile() {
  const { name } = useParams()
  const { user } = useOutletContext<AppContext>()
  const [player, setPlayer] = useState<Player | null>(null)
  const [bots, setBots] = useState<Bot[]>([])
  const [matches, setMatches] = useState<Match[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    if (!name) return
    let alive = true
    const load = () =>
      api.player(name)
        .then(d => { if (alive) { setPlayer(d.player); setBots(d.bots); setMatches(d.matches) } })
        .catch(e => alive && setError(String(e)))
    load()
    const t = setInterval(load, 6000)
    return () => { alive = false; clearInterval(t) }
  }, [name])

  if (error) return <div className="error-box">{error}</div>
  if (!player) return <p className="muted">Loading…</p>

  const fullName = [player.firstName, player.lastName].filter(Boolean).join(' ')

  return (
    <>
      <div className="page-head">
        <h1>{player.name}</h1>
        {fullName && <span className="muted">{fullName}</span>}
      </div>
      <div className="panel">
        <div className="score-row">
          <span className="muted">bots</span> {player.activeBots} active / {player.bots} total
          <span className="muted">best rating</span> {player.bestRating?.toFixed(0) ?? '—'}
          <span className="muted">record</span>
          <span>
            <span className="result-win">{player.wins}W</span> /{' '}
            <span className="result-loss">{player.losses}L</span> / {player.draws}D
            <span className="muted"> in {player.matchesPlayed} games</span>
          </span>
        </div>
      </div>

      <h2>Analytics</h2>
      <PlayerAnalytics name={player.name} />

      <h2>Bots</h2>
      {bots.length === 0 ? <p className="muted">No bots uploaded yet.</p> : (
        <table>
          <thead>
            <tr>
              <th>Bot</th><th>Lang</th><th>Status</th>
              <th className="num">Rating</th><th className="num">W</th>
              <th className="num">L</th><th className="num">Played</th><th></th>
            </tr>
          </thead>
          <tbody>
            {bots.map(b => (
              <tr key={b.id}>
                <td><Link to={`/bots/${b.id}`}>{b.name}</Link></td>
                <td><LangBadge lang={b.language} /></td>
                <td><span className={`badge ${b.status}`}>{b.status}</span></td>
                <td className="num">{b.rating?.toFixed(0) ?? '—'}</td>
                <td className="num result-win">{b.wins ?? 0}</td>
                <td className="num result-loss">{b.losses ?? 0}</td>
                <td className="num muted">{b.matchesPlayed ?? 0}</td>
                <td>{b.status === 'active' && user?.login === player.name &&
                  <Link to={`/challenge?bot=${b.id}`}>challenge with this bot →</Link>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h2>Recent matches</h2>
      <MatchTable matches={matches} />
    </>
  )
}
