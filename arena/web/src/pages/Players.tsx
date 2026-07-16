import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, Player } from '../api'

export default function Players() {
  const [players, setPlayers] = useState<Player[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    const load = () => api.players().then(p => alive && setPlayers(p)).catch(e => alive && setError(String(e)))
    load()
    const t = setInterval(load, 8000)
    return () => { alive = false; clearInterval(t) }
  }, [])

  if (error) return <div className="error-box">{error}</div>
  if (!players) return <p className="muted">Loading…</p>

  return (
    <>
      <h1>Players</h1>
      <table>
        <thead>
          <tr>
            <th>Player</th><th>Best bot</th><th className="num">Best rating</th>
            <th className="num">Bots</th><th className="num">W</th>
            <th className="num">L</th><th className="num">D</th><th className="num">Played</th>
          </tr>
        </thead>
        <tbody>
          {players.map(p => (
            <tr key={p.id}>
              <td><Link to={`/players/${p.name}`}>{p.name}</Link></td>
              <td className="muted">{p.bestBot ?? '—'}</td>
              <td className="num">{p.bestRating?.toFixed(0) ?? '—'}</td>
              <td className="num muted">{p.activeBots}/{p.bots}</td>
              <td className="num result-win">{p.wins}</td>
              <td className="num result-loss">{p.losses}</td>
              <td className="num result-draw">{p.draws}</td>
              <td className="num muted">{p.matchesPlayed}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  )
}
