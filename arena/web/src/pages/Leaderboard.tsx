import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, Bot } from '../api'
import LangBadge from '../components/LangBadge'

const MEDALS = ['🥇', '🥈', '🥉']

export default function Leaderboard() {
  const [bots, setBots] = useState<Bot[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    const load = () => api.leaderboard().then(b => alive && setBots(b)).catch(e => alive && setError(String(e)))
    load()
    const t = setInterval(load, 5000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  if (error) return <div className="error-box">{error}</div>
  if (!bots) return <p className="muted">Loading…</p>

  return (
    <>
      <div className="page-head">
        <h1>Leaderboard</h1>
        <span className="muted">Elo, K=32, everyone starts at 1200</span>
      </div>
      <table>
        <thead>
          <tr>
            <th>#</th><th>Bot</th><th>Owner</th><th>Lang</th>
            <th className="num">Rating</th><th className="num">W</th>
            <th className="num">L</th><th className="num">D</th>
            <th>Win rate</th>
          </tr>
        </thead>
        <tbody>
          {bots.map((b, i) => {
            const played = b.matchesPlayed ?? 0
            const rate = played > 0 ? (b.wins ?? 0) / played : 0
            return (
              <tr key={b.id} className={i < 3 ? 'top-row' : ''}>
                <td className="muted rank">{MEDALS[i] ?? i + 1}</td>
                <td><Link to={`/bots/${b.id}`} className="bot-name">{b.name}</Link></td>
                <td><Link to={`/players/${b.owner}`} className="muted">{b.owner}</Link></td>
                <td><LangBadge lang={b.language} /></td>
                <td className="num rating">{b.rating?.toFixed(0) ?? '—'}</td>
                <td className="num result-win">{b.wins ?? 0}</td>
                <td className="num result-loss">{b.losses ?? 0}</td>
                <td className="num result-draw">{b.draws ?? 0}</td>
                <td className="winrate-cell">
                  <div className="winrate">
                    <div className="winrate-fill" style={{ width: `${rate * 100}%` }} />
                  </div>
                  <span className="muted small-num">{played > 0 ? `${Math.round(rate * 100)}%` : '—'} of {played}</span>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </>
  )
}
